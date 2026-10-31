package daemonthread

import (
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	aguimocks "github.com/inference-gateway/cli/tests/mocks/agui"
	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	browserinfra "github.com/inference-gateway/cli/internal/browser/infrastructure"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	headless "github.com/inference-gateway/cli/internal/presentation/headless"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

const testToken = "secret"

// fakeDaemon is a scripted local daemon: a real binding whose handler answers
// the TUI's frames the way the registry and a thread's worker would.
type fakeDaemon struct {
	t       *testing.T
	binding *agui.Binding
	handler *aguimocks.FakeHandler
	port    int

	mu     sync.Mutex
	runs   func(conn *agui.Conn, input map[string]any)
	export []convdomain.ConversationEntry
	reply  map[string]string
}

// startDaemon starts the scripted daemon on port, a free one when 0.
func startDaemon(t *testing.T, port int) *fakeDaemon {
	t.Helper()
	d := &fakeDaemon{t: t, handler: &aguimocks.FakeHandler{}, reply: map[string]string{}}
	d.handler.HandleCalls(d.handle)
	d.binding = agui.NewBinding(agui.BindingConfig{
		Port: port, Token: testToken, Handshake: browserinfra.BindingHandshake, AckAttrs: map[string]any{"workspace": "laptop"},
	}, d.handler)
	if err := d.binding.Start(); err != nil {
		t.Fatalf("starting the binding: %v", err)
	}
	t.Cleanup(d.binding.Close)
	_, portText, _ := net.SplitHostPort(d.binding.Addr())
	d.port, _ = strconv.Atoi(portText)
	return d
}

// config is a chat config whose binding dials this daemon.
func (d *fakeDaemon) config() *config.Config {
	cfg := config.DefaultConfig()
	cfg.Daemon.Binding = config.BindingConfig{Enabled: true, Token: testToken, Port: d.port}
	return cfg
}

func (d *fakeDaemon) handle(conn *agui.Conn, frame []byte) {
	var f map[string]any
	if json.Unmarshal(frame, &f) != nil {
		d.t.Errorf("undecodable client frame %q", frame)
		return
	}
	d.mu.Lock()
	runs, export, canned := d.runs, d.export, d.reply[f["type"].(string)]
	d.mu.Unlock()
	switch f["type"] {
	case "resume_conversation":
		send(conn, `{"type":"MESSAGES_SNAPSHOT","messages":[]}`)
	case "set_mode":
		send(conn, `{"type":"mode","mode":"`+f["mode"].(string)+`"}`)
	case "select_model":
		send(conn, `{"type":"models","current":"`+f["model"].(string)+`"}`)
	case "export_conversation":
		data, _ := json.Marshal(map[string]any{"type": "conversation_export", "id": f["id"], "entries": export})
		send(conn, string(data))
	case "run_agent_input":
		if runs != nil {
			input, _ := f["input"].(map[string]any)
			runs(conn, input)
		}
	default:
		if canned != "" {
			send(conn, canned)
		}
	}
}

// answer cans the daemon's reply to a frame type.
func (d *fakeDaemon) answer(frameType, reply string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.reply[frameType] = reply
}

func (d *fakeDaemon) setRuns(runs func(conn *agui.Conn, input map[string]any)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.runs = runs
}

func (d *fakeDaemon) setExport(entries []convdomain.ConversationEntry) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.export = entries
}

// frames decodes the client frames of one type the daemon read.
func (d *fakeDaemon) frames(frameType string) []map[string]any {
	var out []map[string]any
	for i := range d.handler.HandleCallCount() {
		_, frame := d.handler.HandleArgsForCall(i)
		var f map[string]any
		if json.Unmarshal(frame, &f) == nil && f["type"] == frameType {
			out = append(out, f)
		}
	}
	return out
}

// conn is the daemon's end of the TUI connection.
func (d *fakeDaemon) conn() *agui.Conn {
	d.t.Helper()
	eventually(d.t, "the TUI to attach", func() bool { return d.handler.AttachCallCount() > 0 })
	return d.handler.AttachArgsForCall(d.handler.AttachCallCount() - 1)
}

func send(conn *agui.Conn, frame string) {
	_ = conn.Send([]byte(frame))
}

// encodeRun renders events the way a worker does and returns the run frames.
func encodeRun(t *testing.T, events ...agentdomain.ChatEvent) []string {
	t.Helper()
	var out strings.Builder
	encoder := headless.NewRunEncoder(&out, headless.RunEncoderDeps{Model: "m1", Repo: &convmocks.FakeConversationRepository{}})
	encoder.Start("conv-1", "run-1")
	for _, event := range events {
		encoder.Handle(event)
	}
	_ = encoder.Finish()
	return strings.Split(strings.TrimSpace(out.String()), "\n")
}

// connected is a thread connected to d that follows conv-1, with the notifier
// it reports to.
func connected(t *testing.T, d *fakeDaemon) (*Thread, *agentdomainmocks.FakeUINotifier) {
	t.Helper()
	thread := New(d.config(), Options{ProjectDir: "/proj"}, nil)
	t.Cleanup(thread.Close)
	notifier := &agentdomainmocks.FakeUINotifier{}
	thread.SetNotifier(notifier)
	if err := thread.Connect(t.Context()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := thread.Follow(t.Context(), "conv-1", sessionsdomain.ThreadOptions{}); err != nil {
		t.Fatalf("Follow: %v", err)
	}
	return thread, notifier
}

// notified collects the UI notifications of type T.
func notified[T any](notifier *agentdomainmocks.FakeUINotifier) []T {
	var out []T
	for i := range notifier.NotifyCallCount() {
		if event, ok := notifier.NotifyArgsForCall(i).(T); ok {
			out = append(out, event)
		}
	}
	return out
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
