package domain

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPortableKeepsTheAgentAndDropsWhatBelongsToTheDaemon(t *testing.T) {
	opened := ThreadOptions{
		Model:              "provider/model",
		Mode:               "auto",
		SystemPrompt:       "prompt",
		CustomInstructions: "instructions",
		MaxTurns:           500,
		SandboxDirectories: []string{"/home/me/project"},
		Subagent:           &Subagent{Parent: "parent", Depth: 1},
		Agent:              "reviewer",
		Tools:              []string{"Read", "Grep"},
		DisallowedTools:    []string{"Bash"},
		Workspace:          "remote",
		WorkspaceDir:       "/workspace/project",
		Env:                []string{"TOKEN=secret"},
	}
	want := ThreadOptions{
		Model:              "provider/model",
		Mode:               "auto",
		SystemPrompt:       "prompt",
		CustomInstructions: "instructions",
		MaxTurns:           500,
		Tools:              []string{"Read", "Grep"},
		DisallowedTools:    []string{"Bash"},
	}
	if got := opened.Portable(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Portable() = %+v, want %+v", got, want)
	}
}

func TestThreadOptionsJSON(t *testing.T) {
	tests := []struct {
		name string
		wire string
		want ThreadOptions
	}{
		{
			name: "a client names the agent only",
			wire: `{"agent":"reviewer","model":"provider/fallback","custom_instructions":"siblings"}`,
			want: ThreadOptions{Agent: "reviewer", Model: "provider/fallback", CustomInstructions: "siblings"},
		},
		{
			name: "a relay carries what the agent resolved to",
			wire: `{"model":"provider/agent","system_prompt":"body","tools":["Read","Grep"],"disallowed_tools":["Bash"]}`,
			want: ThreadOptions{Model: "provider/agent", SystemPrompt: "body", Tools: []string{"Read", "Grep"}, DisallowedTools: []string{"Bash"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got ThreadOptions
			if err := json.Unmarshal([]byte(tt.wire), &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("decoded %+v, want %+v", got, tt.want)
			}
			encoded, err := json.Marshal(got.Portable())
			if err != nil {
				t.Fatal(err)
			}
			var relayed map[string]any
			_ = json.Unmarshal(encoded, &relayed)
			if _, ok := relayed["agent"]; ok {
				t.Fatalf("the portable options name the agent: %s", encoded)
			}
		})
	}
}
