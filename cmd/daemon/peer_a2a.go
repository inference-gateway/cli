package daemon

import (
	"crypto/subtle"
	"crypto/x509"
	"io"
	"net/http"
	"strings"
	"sync"

	version "github.com/inference-gateway/cli/cmd/version"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
	agentteleport "github.com/inference-gateway/cli/internal/agent/teleport"
	a2ainfra "github.com/inference-gateway/cli/internal/protocols/a2a/infrastructure"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
	sessions "github.com/inference-gateway/cli/internal/sessions"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

// peerA2AHost is the a2a peer endpoint a workspace's daemon serves on its
// binding at /a2a, for the workspaces that chose protocol a2a: the frame
// vocabulary over A2A JSON-RPC. A streamed exchange announces this daemon in
// its first event and carries every frame as an A2A message, a SendMessage
// puts one frame on it, and whatever that frame handling produces rides the
// exchange's stream. The binding's token or client certificates authenticate
// it, exactly as the WebSocket route does.
type peerA2AHost struct {
	registry    *sessions.Registry
	teleports   *agentteleport.Service
	attrs       map[string]string
	token       string
	requireCert bool

	mu      sync.Mutex
	streams map[string]*peerA2AStream
}

// newPeerA2AHost builds the endpoint behind /a2a over the handler parts
// startThreads wired, announcing what the binding's ack declares.
func newPeerA2AHost(registry *sessions.Registry, teleports *agentteleport.Service, token string, requireCert bool) *peerA2AHost {
	attrs := make(map[string]string, 3)
	for key, value := range teleports.AckAttrs() {
		if text, ok := value.(string); ok {
			attrs[key] = text
		}
	}
	attrs[agentinfra.AttrVersion] = version.Value()
	return &peerA2AHost{
		registry:    registry,
		teleports:   teleports,
		attrs:       attrs,
		token:       token,
		requireCert: requireCert,
		streams:     make(map[string]*peerA2AStream),
	}
}

func (h *peerA2AHost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.authenticated(r) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		h.serveCard(w, r)
	case http.MethodPost:
		h.serveRequest(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// serveCard is the agent card of the peer endpoint, at the URL the request
// reached it on.
func (h *peerA2AHost) serveCard(w http.ResponseWriter, r *http.Request) {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(a2ainfra.PeerCard(scheme+"://"+r.Host+a2ainfra.PeerPath, h.attrs[agentinfra.AttrVersion]))
}

func (h *peerA2AHost) serveRequest(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, agui.MaxInboundFrameBytes))
	if err != nil {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}
	req, ok := a2ainfra.ParsePeerRequest(body)
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if req.Method == a2ainfra.PeerMethodSend {
		h.serveSend(w, req)
		return
	}
	h.serveStream(w, r, req.Exchange)
}

// serveSend routes one peer frame onto the exchange's stream and answers with
// the completed task A2A returns for SendMessage. Frames the handling
// produces are the response's business never: they ride the stream.
func (h *peerA2AHost) serveSend(w http.ResponseWriter, req a2ainfra.PeerRequest) {
	w.Header().Set("Content-Type", "application/json")
	client := h.streamFor(req.Exchange)
	if client == nil {
		_, _ = w.Write(a2ainfra.PeerError(req.ID, "no peer exchange is open with this contextId"))
		return
	}
	w.WriteHeader(http.StatusOK)
	if !h.teleports.Handle(client, req.Frame) {
		h.registry.Handle(client, req.Frame)
	}
	_, _ = w.Write(a2ainfra.PeerAck(req.ID, req.Exchange))
}

// serveStream binds the exchange: it attaches a client that streams everything
// this daemon puts on it as server-sent events, announces what the ack carries
// in the stream's first event, and detaches when the exchange ends.
func (h *peerA2AHost) serveStream(w http.ResponseWriter, r *http.Request, exchange string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	client := h.register(exchange, w, flusher)
	defer h.detach(exchange, client)
	client.event(a2ainfra.PeerEvent(nil, h.attrs))
	<-r.Context().Done()
}

// register puts the exchange's stream client on the map, where SendMessages
// find it, and attaches it to the registry.
func (h *peerA2AHost) register(exchange string, w http.ResponseWriter, flusher http.Flusher) *peerA2AStream {
	client := &peerA2AStream{w: w, flusher: flusher}
	h.mu.Lock()
	h.streams[exchange] = client
	h.mu.Unlock()
	h.registry.Attach(client)
	return client
}

// detach unregisters the exchange's stream client and lets the registry drop
// the threads it followed.
func (h *peerA2AHost) detach(exchange string, client *peerA2AStream) {
	h.mu.Lock()
	if h.streams[exchange] == client {
		delete(h.streams, exchange)
	}
	h.mu.Unlock()
	h.registry.Detach(client)
}

// streamFor is the stream client of a bound exchange, nil when none is open.
func (h *peerA2AHost) streamFor(exchange string) *peerA2AStream {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.streams[exchange]
}

// authenticated accepts the binding's token or a client certificate the
// listener verified, the same rule the WebSocket hello follows.
func (h *peerA2AHost) authenticated(r *http.Request) bool {
	if h.token != "" {
		presented := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(presented), []byte(h.token)) == 1 {
			return true
		}
	}
	return h.requireCert && r.TLS != nil && len(r.TLS.PeerCertificates) > 0
}

// peerA2AStream is one bound exchange as this daemon's end of it: the far side
// keeps the stream open, and every frame this daemon puts on it is an event.
// Whatever protocol the exchange's other end spoke is its own affair.
type peerA2AStream struct {
	w       http.ResponseWriter
	flusher http.Flusher

	mu   sync.Mutex
	dead bool
}

var (
	_ agentinfra.PeerConn   = (*peerA2AStream)(nil)
	_ sessionsdomain.Client = (*peerA2AStream)(nil)
)

func (c *peerA2AStream) Kind() string { return agentinfra.PeerClientKind }

// Send puts one frame on the exchange for the peer's stream to carry.
func (c *peerA2AStream) Send(frame []byte) error {
	c.event(a2ainfra.PeerEvent(frame, nil))
	return nil
}

// Deliver puts one frame on the exchange, as the registry and the teleports
// answer their requester.
func (c *peerA2AStream) Deliver(frame []byte) {
	c.event(a2ainfra.PeerEvent(frame, nil))
}

// event writes one stream event, unless the exchange ended.
func (c *peerA2AStream) event(event []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dead {
		return
	}
	_, _ = c.w.Write(a2ainfra.SSE(event))
	if c.flusher != nil {
		c.flusher.Flush()
	}
}

// Close marks the exchange dead, so the frames this daemon still owes it are
// dropped rather than written to an ended response.
func (c *peerA2AStream) Close() {
	c.mu.Lock()
	c.dead = true
	c.mu.Unlock()
}

func (c *peerA2AStream) AckAttr(string) string                 { return "" }
func (c *peerA2AStream) PeerCertificates() []*x509.Certificate { return nil }
