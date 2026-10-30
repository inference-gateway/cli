package toolformatter

import (
	"encoding/json"
	"reflect"
	"testing"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	computer "github.com/inference-gateway/cli/internal/computer"
	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
	mcpdomain "github.com/inference-gateway/cli/internal/protocols/mcp/domain"
	tools "github.com/inference-gateway/cli/internal/tools"
)

func TestTypedRehydratesTheDataOfADecodedResult(t *testing.T) {
	tests := []struct {
		tool string
		data any
	}{
		{tools.ToolBash, &agentdomain.BashToolResult{Command: "ls", Output: "a\n", ExitCode: 1}},
		{tools.ToolRead, &agentdomain.FileReadToolResult{FilePath: "a.go", Content: "package a", Size: 9}},
		{tools.ToolWrite, &agentdomain.FileWriteToolResult{FilePath: "a.go", BytesWritten: 9, Created: true}},
		{tools.ToolTodoWrite, &agentdomain.TodoWriteToolResult{Todos: []agentdomain.TodoItem{{ID: "1", Content: "do", Status: "pending"}}}},
		{computer.ToolComputer, &computerdomain.Observation{Message: "clicked", CursorX: 3, Width: 10}},
		{mcpdomain.ToolName("files", "list"), &mcpdomain.ToolResult{}},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			raw, err := json.Marshal(&agentdomain.ToolExecutionResult{ToolName: tt.tool, Success: true, Data: tt.data})
			if err != nil {
				t.Fatal(err)
			}
			var decoded agentdomain.ToolExecutionResult
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatal(err)
			}
			if _, generic := decoded.Data.(map[string]any); !generic {
				t.Fatalf("decoded data is %T, want the generic map JSON gives", decoded.Data)
			}

			got := typed(&decoded)
			if !reflect.DeepEqual(got.Data, tt.data) {
				t.Errorf("typed data = %#v, want %#v", got.Data, tt.data)
			}
			if _, generic := decoded.Data.(map[string]any); !generic {
				t.Error("typed changed the caller's result instead of returning a copy")
			}
		})
	}
}

func TestTypedLeavesOtherResultsAlone(t *testing.T) {
	live := &agentdomain.ToolExecutionResult{ToolName: tools.ToolBash, Data: &agentdomain.BashToolResult{Command: "ls"}}
	unknown := &agentdomain.ToolExecutionResult{ToolName: "Unknown", Data: map[string]any{"x": 1.0}}
	for _, result := range []*agentdomain.ToolExecutionResult{live, unknown, nil} {
		if got := typed(result); got != result {
			t.Errorf("typed(%+v) = %+v, want the same result", result, got)
		}
	}
}
