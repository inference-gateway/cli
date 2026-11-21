package sessions

import (
	"errors"
	"testing"
	"time"

	sessionsmocks "github.com/inference-gateway/cli/tests/mocks/sessions"

	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

func resolveExplore(dir string, opts sessionsdomain.ThreadOptions) (sessionsdomain.ThreadOptions, error) {
	if opts.Agent != "explore" {
		return sessionsdomain.ThreadOptions{}, errors.New(`unknown agent "ghost"; available agents: explore`)
	}
	opts.SystemPrompt, opts.Tools = "explore body from "+dir, []string{"Read", "Grep"}
	return opts, nil
}

func TestRegistrySelectAgentForwardsTheResolvedAgentAndKeepsTheLaunchOptions(t *testing.T) {
	l := &launcher{}
	r := NewRegistry(l.launch, time.Hour)
	r.ResolveAgents(resolveExplore)
	c := &sessionsmocks.FakeClient{}
	handle(r, c, `{"type":"resume_conversation","project_dir":"/proj","id":"conv-1","model":"provider/own"}`)
	worker, lines := l.worker(0)

	handle(r, c, `{"type":"select_agent","agent":"explore","project_dir":"/proj"}`)

	want := `{"type":"select_agent","project_dir":"/proj","system_prompt":"explore body from /proj","agent":"explore","tools":["Read","Grep"]}`
	if worker.SendCallCount() != 2 || string(worker.SendArgsForCall(1)) != want {
		t.Fatalf("worker got %d frames, want the resolved select_agent %s", worker.SendCallCount(), want)
	}
	key := sessionsdomain.ThreadKey{ProjectDir: "/proj", ConversationID: "conv-1"}
	if got := r.Options(key); got.Agent != "" || got.SystemPrompt != "" || got.Model != "provider/own" {
		t.Fatalf("thread options = %+v, want the launch options untouched so the prompt cache holds", got)
	}

	reply := `{"type":"agent","agent":"explore","model":"provider/own"}`
	lines <- []byte(reply)
	receives(t, c, reply)
}

func TestRegistrySelectAgentRefusals(t *testing.T) {
	tests := []struct {
		name    string
		running bool
		frame   string
		want    string
	}{
		{
			name:  "an unknown agent",
			frame: `{"type":"select_agent","agent":"ghost","project_dir":"/proj"}`,
			want:  `{"agent":"ghost","error":"unknown agent \"ghost\"; available agents: explore","type":"agent"}`,
		},
		{
			name:  "no agent named",
			frame: `{"type":"select_agent","project_dir":"/proj"}`,
			want:  `{"agent":"","error":"select_agent needs the agent to switch to","type":"agent"}`,
		},
		{
			name:    "a turn still running",
			running: true,
			frame:   `{"type":"select_agent","agent":"explore","project_dir":"/proj"}`,
			want:    `{"agent":"explore","error":"finish or stop the running turn before switching the main agent","type":"agent"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &launcher{}
			r := NewRegistry(l.launch, time.Hour)
			r.ResolveAgents(resolveExplore)
			c := &sessionsmocks.FakeClient{}
			handle(r, c, `{"type":"resume_conversation","project_dir":"/proj","id":"conv-1"}`)
			worker, lines := l.worker(0)
			var frames []string
			if tt.running {
				started := `{"type":"RUN_STARTED","threadId":"conv-1","runId":"run-1"}`
				lines <- []byte(started)
				frames = append(frames, started)
				eventually(t, "RUN_STARTED", func() bool { return c.DeliverCallCount() == 1 })
			}

			handle(r, c, tt.frame)

			receives(t, c, append(frames, tt.want)...)
			if worker.SendCallCount() != 1 {
				t.Fatalf("a refused switch reached the worker: %d frames", worker.SendCallCount())
			}
		})
	}
}
