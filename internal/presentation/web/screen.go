package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// ScreenPath is where the daemon's binding listener serves a session's latest
// screen frame: GET ScreenPath?id=<session> with the binding token as a bearer,
// and POST ScreenPath?id=<session> to take the screen over, drive it or hand
// it back with the same bearer.
const ScreenPath = "/screen/"

const frameTimeout = 10 * time.Second

// maxScreenBodyBytes bounds a screen POST body: one input, never a stream.
const maxScreenBodyBytes = 64 << 10

// The screen control actions a person's POST carries.
const (
	ScreenActionTakeOver = "take_over"
	ScreenActionHandBack = "hand_back"
)

// ScreenInput is one person-driven input the Screen view sends while it holds
// the session's screen, in the frame space the image is served in. The field
// names are the worker's ScreenInput contract on the wire.
type ScreenInput struct {
	Kind      string `json:"kind"` // move, click, double_click, scroll, type, key
	X         int    `json:"x,omitempty"`
	Y         int    `json:"y,omitempty"`
	Text      string `json:"text,omitempty"`
	Combo     string `json:"combo,omitempty"`
	Button    string `json:"button,omitempty"`
	Direction string `json:"direction,omitempty"`
	Amount    int    `json:"amount,omitempty"`
}

// ScreenOutcome is the worker's answer to a screen control or input call: the
// screen's held state after it and what failed, if it came to that.
type ScreenOutcome struct {
	Held  bool
	Error string
}

// Screen serves the latest screen frame of a session's worker as an image, for
// the web terminal's live view, and its take-over: the control action and the
// person's inputs while held. Frame asks the daemon for it wherever the
// session runs; Control and Input relay to the same worker.
type Screen struct {
	Token       string
	ClientCerts bool
	Frame       func(ctx context.Context, sessionID string) (*agentdomain.Frame, error)
	Control     func(ctx context.Context, sessionID, action string) (*ScreenOutcome, error)
	Input       func(ctx context.Context, sessionID string, input ScreenInput) (*ScreenOutcome, error)
}

func (s *Screen) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.serveFrame(w, r)
	case http.MethodPost:
		s.serveControl(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// serveFrame answers the GET the Screen view polls: the latest frame as an
// image, its frame space in the X-Frame-Width and X-Frame-Height headers a
// take-over maps the person's pointer with.
func (s *Screen) serveFrame(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(w, r) {
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), frameTimeout)
	defer cancel()
	frame, err := s.Frame(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	data, err := base64.StdEncoding.DecodeString(frame.Data)
	if err != nil {
		http.Error(w, "frame is not base64", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", imageType(frame.Format))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Frame-Width", strconv.Itoa(frame.Width))
	w.Header().Set("X-Frame-Height", strconv.Itoa(frame.Height))
	w.Header().Set("X-Frame-Timestamp", frame.Timestamp.UTC().Format(time.RFC3339))
	_, _ = w.Write(data)
}

// serveControl answers a POST: a take over, a hand back or one screen input
// for the session the id names. An input for a screen nobody holds, and a
// take-over of a screen somebody else holds, are conflicts (409), so a stale
// tab cannot drive what it handed back.
func (s *Screen) serveControl(w http.ResponseWriter, r *http.Request) {
	if s.Control == nil || s.Input == nil {
		http.Error(w, "the screen route is read-only here", http.StatusNotImplemented)
		return
	}
	if !s.authorized(w, r) {
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxScreenBodyBytes))
	if err != nil {
		http.Error(w, "unreadable body", http.StatusBadRequest)
		return
	}
	var request struct {
		Action string       `json:"action"`
		Input  *ScreenInput `json:"input"`
	}
	if json.Unmarshal(body, &request) != nil {
		http.Error(w, "unreadable body", http.StatusBadRequest)
		return
	}

	isControl := request.Input == nil
	if isControl && request.Action != ScreenActionTakeOver && request.Action != ScreenActionHandBack {
		http.Error(w, "unknown screen action "+request.Action, http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), frameTimeout)
	defer cancel()
	var outcome *ScreenOutcome
	if isControl {
		outcome, err = s.Control(ctx, id, request.Action)
	} else {
		outcome, err = s.Input(ctx, id, *request.Input)
	}
	if err != nil {
		status := http.StatusBadGateway
		if strings.Contains(err.Error(), "is not open") {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	if status, conflictErr := screenStatus(outcome, isControl); conflictErr != "" {
		http.Error(w, conflictErr, status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]bool{"held": outcome.Held})
}

// screenStatus resolves a reply's status. Both held-state conflicts are 409s,
// which is the stale-tab guard; anything the worker could not drive is a
// failure of this screen call.
func screenStatus(outcome *ScreenOutcome, isControl bool) (int, string) {
	if outcome.Error == "" {
		return http.StatusOK, ""
	}
	switch {
	case isControl && outcome.Held, !isControl && !outcome.Held:
		return http.StatusConflict, outcome.Error
	default:
		return http.StatusInternalServerError, outcome.Error
	}
}

// authorized gates both methods with the binding token as a bearer.
func (s *Screen) authorized(w http.ResponseWriter, r *http.Request) bool {
	bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !authorized(r, bearer, s.Token, s.ClientCerts) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

// imageType is the Content-Type a frame is served with. The format comes from
// the session's worker, which may run on another daemon, so only the image
// types the screen sources produce pass and anything else is served as jpeg.
func imageType(format string) string {
	if format == "png" {
		return "image/png"
	}
	return "image/jpeg"
}
