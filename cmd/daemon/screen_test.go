package daemon

import (
	"bytes"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	sessionsmocks "github.com/inference-gateway/cli/tests/mocks/sessions"

	sessions "github.com/inference-gateway/cli/internal/sessions"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

func TestScreenRelaysReachTheSessionWorker(t *testing.T) {
	projectDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	lines := make(chan []byte, 16)
	sent := make(chan []byte, 16)
	worker := &sessionsmocks.FakeWorker{}
	worker.LinesReturns(lines)
	worker.StopCalls(sync.OnceFunc(func() { close(lines) }))
	worker.SendCalls(func(line []byte) error {
		sent <- bytes.Clone(line)
		return nil
	})
	registry := sessions.NewRegistry(func(sessionsdomain.ThreadKey, sessionsdomain.ThreadOptions) (sessionsdomain.Worker, error) {
		return worker, nil
	}, time.Minute)
	registry.Handle(&sessionsmocks.FakeClient{}, []byte(`{"type":"resume_conversation","project_dir":"`+projectDir+`","id":"s1"}`))

	errs := make(chan error, 1)
	held := make(chan bool, 1)
	go func() {
		outcome, relayErr := screenControl(registry)(t.Context(), "s1", "take_over")
		errs <- relayErr
		held <- outcome != nil && outcome.Held && outcome.Error == ""
	}()

	var frame map[string]any
	for frame["type"] != "screen_control" {
		if json.Unmarshal(<-sent, &frame) != nil {
			t.Fatal("the relay sent an unreadable frame")
		}
	}
	if frame["action"] != "take_over" || frame["id"] != "s1" || frame["project_dir"] != projectDir {
		t.Fatalf("the relayed frame = %v, want the take over of s1 in %s", frame, projectDir)
	}

	lines <- []byte(`{"type":"screen_result","id":"s1","held":true}`)
	if err := <-errs; err != nil {
		t.Fatalf("the relay call failed: %v", err)
	}
	if !<-held {
		t.Fatal("the relay answer did not read held")
	}
}
