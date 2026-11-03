package infrastructure

import (
	"bufio"
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"

	uuid "github.com/google/uuid"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	a2ainfra "github.com/inference-gateway/cli/internal/protocols/a2a/infrastructure"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

const (
	peerA2AHandshakeTimeout = 10 * time.Second
	peerA2ASendTimeout      = 30 * time.Second
)

// readSSEData reads the payload of the next `data:` line, skipping the SSE
// framing lines of everything else.
func readSSEData(reader *bufio.Reader) ([]byte, error) {
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if payload, ok := strings.CutPrefix(strings.TrimRight(line, "\n\r"), "data:"); ok {
			return []byte(strings.TrimSpace(payload)), nil
		}
	}
}

// a2aPeerInfo is what the stream's announcing event declared about the host.
func a2aPeerInfo(metadata map[string]string) PeerInfo {
	return PeerInfo{
		Workspace: metadata[AttrWorkspace],
		Version:   metadata[AttrVersion],
		StorageID: metadata[AttrStorageID],
	}
}

// a2aPeerClient is the dialing end of an a2a peer exchange. Its stream carries
// every frame the peer puts on it, announced first, and every Send is a
// SendMessage whose produced frames the peer streams back. It lives until it
// is closed, beyond the context that dialed it, which sagas and relays
// outlive only through the exchange itself.
type a2aPeerClient struct {
	url      string
	me       string
	token    string
	exchange string
	handler  agui.Handler

	http      *http.Client
	streamCtx context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	closeOnce sync.Once

	certificates []*x509.Certificate
	attrs        map[string]string
}

var (
	_ PeerConn = (*a2aPeerClient)(nil)
	_ PeerConn = (*agui.Conn)(nil)
)

// dialPeerA2A opens the exchange a workspace serves over the a2a protocol. The
// stream's first event announces the peer, which is also how a daemon that
// serves nothing over a2a fails, before the exchange can carry anything.
func dialPeerA2A(ctx context.Context, endpoint agentdomain.Endpoint, handler agui.Handler, me string) (PeerConn, PeerInfo, error) {
	scheme := "http"
	if endpoint.TLS != nil {
		scheme = "https"
	}
	c := &a2aPeerClient{
		url:      fmt.Sprintf("%s://%s%s", scheme, endpoint.Addr, a2ainfra.PeerPath),
		me:       me,
		token:    endpoint.Token,
		exchange: uuid.NewString(),
		handler:  handler,
		done:     make(chan struct{}),
		http: &http.Client{
			Transport: &http.Transport{TLSClientConfig: endpoint.TLS, ResponseHeaderTimeout: peerA2AHandshakeTimeout},
		},
	}
	c.streamCtx, c.cancel = context.WithCancel(context.WithoutCancel(ctx))
	resp, err := c.stream()
	if err != nil {
		c.cancel()
		return nil, PeerInfo{}, err
	}
	reader := bufio.NewReader(resp.Body)
	handler.Attach(nil)
	if err := c.announce(reader); err != nil {
		_ = resp.Body.Close()
		c.cancel()
		handler.Detach(nil)
		return nil, PeerInfo{}, err
	}
	c.certificates = peerCertificatesOf(resp)
	go c.readFrames(reader, resp.Body)
	return c, a2aPeerInfo(c.attrs), nil
}

// stream posts the SendStreamingMessage request that binds the exchange, whose
// response body carries the peer's frames as server-sent events. The bearer
// token stands in for the hello when the binding has one.
func (c *a2aPeerClient) stream() (*http.Response, error) {
	req, err := c.request(c.streamCtx, a2ainfra.PeerMethodStream, []byte(`{"type":"peer_bind"}`))
	if err != nil {
		return nil, fmt.Errorf("reaching the workspace's daemon: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reaching the workspace's daemon: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("the workspace's daemon does not serve the a2a protocol on %s (%s)", c.url, resp.Status)
	}
	if mediaType, _, mediaErr := mime.ParseMediaType(resp.Header.Get("Content-Type")); mediaErr != nil || mediaType != "text/event-stream" {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("the workspace's daemon does not serve the a2a protocol on %s (%q instead of text/event-stream)", c.url, mediaType)
	}
	return resp, nil
}

// announce reads the stream's first event, which the host answers with what
// hello's ack declares over the AG-UI binding: its identity facts.
func (c *a2aPeerClient) announce(reader *bufio.Reader) error {
	c.attrs = make(map[string]string, 3)
	for {
		data, err := readSSEData(reader)
		if err != nil {
			return fmt.Errorf("the workspace's daemon did not announce itself over a2a: %w", err)
		}
		frame, metadata, hasFrame := a2ainfra.DecodePeerEvent(data)
		for key, value := range metadata {
			c.attrs[key] = value
		}
		if hasFrame {
			c.handler.Handle(nil, frame)
		}
		if c.attrs[AttrWorkspace] != "" {
			return nil
		}
	}
}

// readFrames reads the frames the peer puts on the stream until it ends, and
// hands them to the handler, exactly as the AG-UI binding does per socket.
func (c *a2aPeerClient) readFrames(reader *bufio.Reader, body io.ReadCloser) {
	defer func() {
		_ = body.Close()
		c.Close()
		c.handler.Detach(nil)
	}()
	for {
		data, err := readSSEData(reader)
		if err != nil {
			return
		}
		if frame, _, hasFrame := a2ainfra.DecodePeerEvent(data); hasFrame {
			c.handler.Handle(nil, frame)
		}
	}
}

// Send puts one frame on the peer as a SendMessage. The frames its handling
// produces arrive on the stream, not in the reply, which is the completed task
// A2A answers with.
func (c *a2aPeerClient) Send(frame []byte) error {
	select {
	case <-c.done:
		return errors.New("the workspace's daemon is disconnected")
	default:
	}
	requestCtx, cancel := context.WithTimeout(c.streamCtx, peerA2ASendTimeout)
	defer cancel()
	req, err := c.request(requestCtx, a2ainfra.PeerMethodSend, frame)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("the workspace's daemon did not take the frame: %w", err)
	}
	reply, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
	if readErr != nil {
		return fmt.Errorf("the workspace's daemon did not take the frame: %w", readErr)
	}
	refused := a2ainfra.DecodePeerReply(reply)
	switch {
	case resp.StatusCode != http.StatusOK && refused != nil:
		return fmt.Errorf("the workspace's daemon refused the frame: %w", refused)
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("the workspace's daemon refused the frame: %s", resp.Status)
	}
	return refused
}

// Deliver is the host role of a peer conn. The dialing end receives frames on
// its stream, so it never delivers one; only the contract keeps the method.
func (c *a2aPeerClient) Deliver(frame []byte) {}

// AckAttr is one other field the announcing event declared, empty when it
// omitted it.
func (c *a2aPeerClient) AckAttr(key string) string {
	return c.attrs[key]
}

// PeerCertificates are the certificates the peer presented over TLS, nil on a
// plain connection.
func (c *a2aPeerClient) PeerCertificates() []*x509.Certificate {
	return c.certificates
}

// Kind is the daemon kind every peer conn declares.
func (c *a2aPeerClient) Kind() string {
	return PeerClientKind
}

// Close ends the exchange. Its stream read then ends and detaches the handler,
// as an AG-UI socket's does.
func (c *a2aPeerClient) Close() {
	c.closeOnce.Do(func() {
		close(c.done)
		c.cancel()
	})
}

// request is one JSON-RPC POST to the peer endpoint carrying the frame on this
// exchange, authenticated by the bearer token when the workspace's entry has
// one, else by the client certificate its TLS config already presents.
func (c *a2aPeerClient) request(ctx context.Context, method a2ainfra.PeerMethod, frame []byte) (*http.Request, error) {
	body := a2ainfra.EncodePeerRequest(method, c.exchange, frame, map[string]string{AttrWorkspace: c.me})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// peerCertificatesOf are the certificates the server presented on a TLS
// connection, nil on a plain one.
func peerCertificatesOf(resp *http.Response) []*x509.Certificate {
	if resp == nil || resp.TLS == nil {
		return nil
	}
	return resp.TLS.PeerCertificates
}
