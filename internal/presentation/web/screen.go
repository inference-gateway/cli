package web

import (
	"context"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// ScreenPath is where the daemon's binding listener serves a session's latest
// screen frame: GET ScreenPath?id=<session> with the binding token as a bearer.
const ScreenPath = "/screen/"

const frameTimeout = 10 * time.Second

// Screen serves the latest screen frame of a session's worker as an image, for
// the web terminal's live view. Frame asks the daemon for it wherever the
// session runs.
type Screen struct {
	Token       string
	ClientCerts bool
	Frame       func(ctx context.Context, sessionID string) (*agentdomain.Frame, error)
}

func (s *Screen) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !authorized(r, bearer, s.Token, s.ClientCerts) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
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

// imageType is the Content-Type a frame is served with. The format comes from
// the session's worker, which may run on another daemon, so only the image
// types the screen sources produce pass and anything else is served as jpeg.
func imageType(format string) string {
	if format == "png" {
		return "image/png"
	}
	return "image/jpeg"
}
