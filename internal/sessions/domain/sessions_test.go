package domain

import (
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
	}
	if got := opened.Portable(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Portable() = %+v, want %+v", got, want)
	}
}
