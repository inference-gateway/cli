package daemon

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	sessions "github.com/inference-gateway/cli/internal/sessions"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
	web "github.com/inference-gateway/cli/internal/presentation/web"
	sessionsmocks "github.com/inference-gateway/cli/tests/mocks/sessions"
)

// relayLauncher is the sessions launcher the screen relay test runs through:
// a fake worker per launch, whose stdout is a channel the test writes into.
type relayLauncher struct {
	mu      sync.Mutex
	workers []*sessionsmocks.FakeWorker
	lines   []chan []byte
}

func (l *relayLauncher) launch(_ sessionsdomain.ThreadKey, _ sessionsdomain.ThreadOptions) (sessionsdomain.Worker, error) {
	lines := make(chan []byte, 16)
	w := &sessionsmocks.FakeWorker{}
	w.LinesReturns(lines)
	w.StopCalls(sync.OnceFunc(func() { close(lines) }))
	l.mu.Lock()
	defer l.mu.Unlock()
	l.workers = append(l.workers, w)
	l.lines = append(l.lines, lines)
	return w, nil
}

func TestScreenRelaysReachTheSessionWorker(t *testing.T) {
	projectDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	launch := &relayLauncher{}
	registry := sessions.NewRegistry(launch.launch, time.Minute)
	registry.Handle(&sessionsmocks.FakeClient{}, []byte(`{"type":"resume_conversation","project_dir":"`+projectDir+`","id":"s1"}`))

	launch.mu.Lock()
	worker, lines := launch.workers[0], launch.lines[0]
	launch.mu.Unlock()

	outcomes := make(chan *web.ScreenOutcome, 1)
	errs := make(chan error, 1)
	control := screenControl(registry)
	go func() {
		outcome, relayErr := control(context.Background(), "s1", "take_over")
		outcomes <- outcome
		errs <- relayErr
	}()

	deadline := time.Now().Add(2 * time.Second)
	for worker.SendCallCount() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("the relay never reached the worker (sends %d)", worker.SendCallCount())
		}
		time.Sleep(5 * time.Millisecond)
	}
	sent := worker.SendArgsForCall(1)
	var frame map[string]any
	if json.Unmarshal(sent, &frame) != nil {
		t.Fatalf("the relay sent an unreadable frame %q", sent)
	}
	if frame["type"] != "screen_control" || frame["action"] != "take_over" || frame["id"] != "s1" || frame["project_dir"] != projectDir {
		t.Fatalf("the relayed frame = %v, want the take over of s1 in %s", frame, projectDir)
	}

	lines <- []byte(`{"type":"screen_result","id":"s1","held":true}`)
	if err := <-errs; err != nil {
		t.Fatalf("the relay call failed: %v", err)
	}
	outcome := <-outcomes
	if outcome == nil || !outcome.Held || outcome.Error != "" {
		t.Fatalf("the relay answer = (%+v, err %v), want held", outcome, outcome)
	}
}
