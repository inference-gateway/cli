package infrastructure

import (
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
			wantEnv:  []string{"PWD=/work/proj"},
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
