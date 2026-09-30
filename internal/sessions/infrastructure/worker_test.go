package infrastructure

import (
	"io"
	"slices"
	"testing"

	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

func TestWorkerArgsAndEnv(t *testing.T) {
	key := sessionsdomain.ThreadKey{ProjectDir: "/work/proj", ConversationID: "conv-1"}
	tests := []struct {
		name     string
		opts     sessionsdomain.ThreadOptions
		wantArgs []string
		wantEnv  []string
	}{
		{
			name:     "no options keep the project's config",
			wantArgs: []string{"headless", "--serve", "--require-approval", "--session-id", "conv-1"},
			wantEnv:  []string{"PWD=/work/proj", "INFER_LOG_STDERR_JSON=true"},
		},
		{
			name: "every option",
			opts: sessionsdomain.ThreadOptions{
				Model:              "openai/gpt-4o",
				Mode:               "plan",
				SystemPrompt:       "be brief",
				CustomInstructions: "use tabs",
				SandboxDirectories: []string{"/work/proj", "/tmp/with space"},
				MaxTurns:           7,
			},
			wantArgs: []string{"headless", "--serve", "--require-approval", "--session-id", "conv-1", "--model", "openai/gpt-4o", "--mode", "plan"},
			wantEnv: []string{
				"PWD=/work/proj",
				"INFER_LOG_STDERR_JSON=true",
				"INFER_PROMPTS_AGENT_SYSTEM_PROMPT=be brief",
				"INFER_PROMPTS_AGENT_CUSTOM_INSTRUCTIONS=use tabs",
				"INFER_TOOLS_SANDBOX_DIRECTORIES=/work/proj\n/tmp/with space",
				"INFER_AGENT_MAX_TURNS=7",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := workerArgs(key, tt.opts); !slices.Equal(got, tt.wantArgs) {
				t.Errorf("workerArgs = %q, want %q", got, tt.wantArgs)
			}
			if got := workerEnv(key, tt.opts); !slices.Equal(got, tt.wantEnv) {
				t.Errorf("workerEnv = %q, want %q", got, tt.wantEnv)
			}
		})
	}
}

// TestWorkerHangUpInterruptsAndClosesStdin checks the stop signal a worker gets
// on every platform: an interrupt frame for the running turn, then stdin EOF.
func TestWorkerHangUpInterruptsAndClosesStdin(t *testing.T) {
	reader, writer := io.Pipe()
	w := &processWorker{stdin: writer}
	go func() { _ = w.hangUp() }()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading the worker's stdin: %v", err)
	}
	if string(got) != "{\"type\":\"interrupt\"}\n" {
		t.Fatalf("the worker read %q, want one interrupt line then EOF", got)
	}
}
