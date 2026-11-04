package web

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	pty "github.com/creack/pty"
	websocket "github.com/gorilla/websocket"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

//go:embed static/*
var staticFiles embed.FS

//go:embed templates/index.html
var indexPage []byte

// Path is where the daemon's binding listener serves the terminal.
const Path = "/terminal/"

// URL is where a browser on this machine opens the terminal, carrying the
// token the page sends in its init frame.
func URL(binding config.Binding, token string) string {
	scheme := "http"
	if binding.TLS.Mode != "" && binding.TLS.Mode != config.TLSModeOff {
		scheme = "https"
	}
	u := url.URL{Scheme: scheme, Host: binding.Addr(), Path: Path}
	if token != "" {
		u.RawQuery = url.Values{"token": {token}}.Encode()
	}
	return u.String()
}

const (
	initTimeout = 3 * time.Second
	termExit    = 2 * time.Second
	sigtermExit = 10 * time.Second
)

// Terminal serves the browser terminal under Path on the daemon's binding
// listener: the xterm page, and one PTY running Command per socket. A token
// matching Token, or a verified client certificate when ClientCerts is set,
// authorizes a socket.
type Terminal struct {
	Token       string
	ClientCerts bool
	Command     func() *exec.Cmd

	once sync.Once
	mux  *http.ServeMux
}

func (t *Terminal) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t.once.Do(func() {
		static, _ := fs.Sub(staticFiles, "static")
		t.mux = http.NewServeMux()
		t.mux.Handle(Path+"static/", http.StripPrefix(Path+"static/", http.FileServer(http.FS(static))))
		t.mux.HandleFunc(Path+"ws", t.serveSocket)
		t.mux.HandleFunc(Path+"{$}", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(indexPage)
		})
	})
	t.mux.ServeHTTP(w, r)
}

type initFrame struct {
	Type  string `json:"type"`
	Token string `json:"token"`
	Cols  uint16 `json:"cols"`
	Rows  uint16 `json:"rows"`
}

func (t *Terminal) authorized(r *http.Request, token string) bool {
	if t.Token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(t.Token)) == 1 {
		return true
	}
	return t.ClientCerts && r.TLS != nil && len(r.TLS.PeerCertificates) > 0
}

func (t *Terminal) serveSocket(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()

	_ = conn.SetReadDeadline(time.Now().Add(initTimeout))
	var init initFrame
	if conn.ReadJSON(&init) != nil || init.Type != "init" || !t.authorized(r, init.Token) {
		logger.Warn("web terminal rejected a socket with a bad or missing init", "remote", r.RemoteAddr)
		return
	}
	_ = conn.SetReadDeadline(time.Time{})

	cmd := t.Command()
	cmd.Env = append(cmd.Environ(), "TERM=xterm-256color")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: max(init.Rows, 1), Cols: max(init.Cols, 1)})
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("Failed to start terminal: "+err.Error()))
		return
	}
	logger.Info("web terminal session started", "pid", cmd.Process.Pid, "remote", r.RemoteAddr)
	defer stop(cmd, ptmx)

	go pump(conn, ptmx)
	for {
		kind, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if kind == websocket.TextMessage && resize(ptmx, data) {
			continue
		}
		if _, err := ptmx.Write(data); err != nil {
			return
		}
	}
}

// pump copies PTY output to the socket until either end closes.
func pump(conn *websocket.Conn, ptmx *os.File) {
	buf := make([]byte, 32*1024)
	for {
		n, err := ptmx.Read(buf)
		if n > 0 {
			if conn.WriteMessage(websocket.BinaryMessage, buf[:n]) != nil {
				return
			}
		}
		if err != nil {
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
			return
		}
	}
}

// resize applies a resize frame and reports whether data was one.
func resize(ptmx *os.File, data []byte) bool {
	var frame struct {
		Type string `json:"type"`
		Cols uint16 `json:"cols"`
		Rows uint16 `json:"rows"`
	}
	if json.Unmarshal(data, &frame) != nil || frame.Type != "resize" {
		return false
	}
	_ = pty.Setsize(ptmx, &pty.Winsize{Rows: frame.Rows, Cols: frame.Cols})
	return true
}

// stop closes the PTY, which hangs up the TUI, and escalates to SIGTERM and
// SIGKILL when it lingers.
// ponytail: the daemon's own exit closes every PTY master the same way, so no
// registry of children. Add one if orphaned TUIs ever show up.
func stop(cmd *exec.Cmd, ptmx *os.File) {
	_ = ptmx.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := cmd.Wait(); err != nil && !errors.Is(err, io.EOF) {
			logger.Debug("web terminal session exited", "pid", cmd.Process.Pid, "error", err)
		}
	}()
	for _, step := range []struct {
		wait   time.Duration
		signal func() error
	}{
		{termExit, func() error { return cmd.Process.Signal(syscall.SIGTERM) }},
		{sigtermExit, cmd.Process.Kill},
	} {
		select {
		case <-done:
			return
		case <-time.After(step.wait):
			_ = step.signal()
		}
	}
	<-done
}
