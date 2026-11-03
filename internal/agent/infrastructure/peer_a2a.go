package infrastructure

import (
	"bufio"
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
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
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

// The a2a peer transport: the endpoints and JSON-RPC methods two infer daemons
// speak when a workspace serves its teleport vocabulary over A2A JSON-RPC
// instead of the AG-UI WebSocket. Frames travel as text parts of A2A messages
// on a streamed exchange; every message/send POST puts one on it.
const (
	PeerA2APath         = "/a2a"
	PeerA2AMethodSend   = "message/send"
	PeerA2AMethodStream = "message/stream"

	peerA2AHandshakeTimeout = 10 * time.Second
	peerA2ASendTimeout      = 30 * time.Second
)

// The roles a peer's messages carry.
const (
	peerA2ARoleUser  = "ROLE_USER"
	peerA2ARoleAgent = "ROLE_AGENT"
)

type peerA2APart struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

type peerA2AMessage struct {
	Kind      string            `json:"kind"`
	MessageID string            `json:"messageId"`
	Role      string            `json:"role"`
	Parts     []peerA2APart     `json:"parts"`
	ContextID string            `json:"contextId,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type peerA2AParams struct {
	Message peerA2AMessage `json:"message"`
}

type peerA2ARequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      string        `json:"id"`
	Method  string        `json:"method"`
	Params  peerA2AParams `json:"params"`
}

type peerA2ATaskStatus struct {
	State string `json:"state"`
}

type peerA2ATask struct {
	ID        string            `json:"id"`
	ContextID string            `json:"contextId"`
	Kind      string            `json:"kind"`
	Status    peerA2ATaskStatus `json:"status"`
}

type peerA2ARPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type peerA2AResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      string           `json:"id"`
	Result  *peerA2ATask     `json:"result,omitempty"`
	Error   *peerA2ARPCError `json:"error,omitempty"`
}

// ParsePeerA2ARequest reads a JSON-RPC request the a2a peer endpoint receives
// and returns its id, method, the exchange in the message's contextId, the
// teleport frame in the message's first text part and the workspace the caller
// announced. Non peer methods are not ok.
func ParsePeerA2ARequest(body []byte) (id, method, exchange string, frame []byte, caller string, ok bool) {
	var req peerA2ARequest
	if err := json.Unmarshal(body, &req); err != nil {
		return "", "", "", nil, "", false
	}
	switch req.Method {
	case PeerA2AMethodSend, PeerA2AMethodStream:
	default:
		return "", "", "", nil, "", false
	}
	exchange = req.Params.Message.ContextID
	caller = req.Params.Message.Metadata[AttrWorkspace]
	if text := peerA2ATextPart(req.Params.Message.Parts); text != "" {
		frame = []byte(text)
	}
	return req.ID, req.Method, exchange, frame, caller, true
}

// PeerA2AEvent wraps one teleport frame as the A2A message event a peer stream
// carries. The event that announces the host carries metadata and no frame.
func PeerA2AEvent(frame []byte, metadata map[string]string) []byte {
	event := peerA2AMessage{
		Kind:      "message",
		MessageID: uuid.NewString(),
		Role:      peerA2ARoleAgent,
		Metadata:  metadata,
	}
	if len(frame) > 0 {
		event.Parts = []peerA2APart{{Kind: "text", Text: string(frame)}}
	}
	raw, _ := json.Marshal(event)
	return raw
}

// PeerA2AAck answers a message/send with the completed task A2A returns. The
// task's artifacts stay empty, because every frame rides the stream.
func PeerA2AAck(id string) []byte {
	raw, _ := json.Marshal(peerA2AResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result: &peerA2ATask{
			ID:        "task-" + id,
			ContextID: "infer-peer-" + id,
			Kind:      "task",
			Status:    peerA2ATaskStatus{State: "completed"},
		},
	})
	return raw
}

// PeerA2AError answers an a2a request it cannot serve with the JSON-RPC error
// A2A returns for an invalid request.
func PeerA2AError(id, message string) []byte {
	raw, _ := json.Marshal(peerA2AResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &peerA2ARPCError{Code: -32602, Message: message},
	})
	return raw
}

// peerA2ATextPart is the message's first text part, "" without one.
func peerA2ATextPart(parts []peerA2APart) string {
	if len(parts) > 0 && parts[0].Kind == "text" {
		return parts[0].Text
	}
	return ""
}

// unwrapPeerA2AEvent reads the A2A message one SSE data line carries, returning
// its frame and the metadata, which only the announcing event carries.
func unwrapPeerA2AEvent(data []byte) (frame []byte, metadata map[string]string, hasFrame bool) {
	var event peerA2AMessage
	if err := json.Unmarshal(data, &event); err != nil || event.Kind != "message" {
		return nil, nil, false
	}
	if text := peerA2ATextPart(event.Parts); text != "" {
		return []byte(text), event.Metadata, true
	}
	return nil, event.Metadata, false
}

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
// message/send whose produced frames the peer streams back. It lives until it
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
		url:      fmt.Sprintf("%s://%s%s", scheme, endpoint.Addr, PeerA2APath),
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
	if err := c.announce(reader, resp); err != nil {
		_ = resp.Body.Close()
		c.cancel()
		return nil, PeerInfo{}, err
	}
	c.certificates = peerCertificatesOf(resp)
	handler.Attach(nil)
	go c.readFrames(reader, resp.Body)
	return c, a2aPeerInfo(c.attrs), nil
}

// stream posts the message/stream request that binds the exchange, whose
// response body carries the peer's frames as server-sent events. The bearer
// token stands in for the hello when the binding has one.
func (c *a2aPeerClient) stream() (*http.Response, error) {
	body := c.requestBody(PeerA2AMethodStream, []byte(`{"type":"peer_bind"}`))
	req, err := http.NewRequestWithContext(c.streamCtx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("reaching the workspace's daemon: %w", err)
	}
	c.authorize(req)
	req.Header.Set("Content-Type", "application/json")
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
func (c *a2aPeerClient) announce(reader *bufio.Reader, resp *http.Response) error {
	c.attrs = make(map[string]string, 3)
	for {
		data, err := readSSEData(reader)
		if err != nil {
			return fmt.Errorf("the workspace's daemon did not announce itself over a2a: %w", err)
		}
		frame, metadata, hasFrame := unwrapPeerA2AEvent(data)
		for key, value := range metadata {
			c.attrs[key] = value
		}
		if hasFrame && c.handler != nil {
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
		frame, _, hasFrame := unwrapPeerA2AEvent(data)
		if hasFrame {
			c.handler.Handle(nil, frame)
		}
	}
}

// Send puts one frame on the peer as a message/send. The frames its handling
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
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, c.url, bytes.NewReader(c.requestBody(PeerA2AMethodSend, frame)))
	if err != nil {
		return err
	}
	c.authorize(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("the workspace's daemon did not take the frame: %w", err)
	}
	reply, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
	if readErr != nil {
		return fmt.Errorf("the workspace's daemon did not take the frame: %w", readErr)
	}
	var acknowledgement peerA2AResponse
	_ = json.Unmarshal(reply, &acknowledgement)
	if resp.StatusCode != http.StatusOK {
		if acknowledgement.Error != nil {
			return fmt.Errorf("the workspace's daemon refused the frame: %s", acknowledgement.Error.Message)
		}
		return fmt.Errorf("the workspace's daemon refused the frame: %s", resp.Status)
	}
	if acknowledgement.Error != nil {
		return errors.New(acknowledgement.Error.Message)
	}
	return nil
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

// requestBody encodes one JSON-RPC request carrying the frame, which the
// bind's peer_bind placeholder stands in for on the opening stream.
func (c *a2aPeerClient) requestBody(method string, frame []byte) []byte {
	message := peerA2AMessage{
		Kind:      "message",
		MessageID: uuid.NewString(),
		Role:      peerA2ARoleUser,
		ContextID: c.exchange,
		Metadata:  map[string]string{AttrWorkspace: c.me},
	}
	if len(frame) > 0 {
		message.Parts = []peerA2APart{{Kind: "text", Text: string(frame)}}
	}
	raw, _ := json.Marshal(peerA2ARequest{
		JSONRPC: "2.0",
		ID:      uuid.NewString(),
		Method:  method,
		Params:  peerA2AParams{Message: message},
	})
	return raw
}

// authorize authenticates the request to the binding: a bearer token when the
// workspace's entry carries one, else the client certificate its TLS config
// already presents.
func (c *a2aPeerClient) authorize(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

// peerCertificatesOf are the certificates the server presented on a TLS
// connection, nil on a plain one.
func peerCertificatesOf(resp *http.Response) []*x509.Certificate {
	if resp == nil || resp.TLS == nil {
		return nil
	}
	return resp.TLS.PeerCertificates
}
