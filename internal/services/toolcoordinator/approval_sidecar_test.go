package toolcoordinator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"

	sdk "github.com/inference-gateway/sdk"
)

func TestSubagentApprovalSidecar_WriteAndClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "approval.json")
	t.Setenv(scheddomain.EnvSubagentApprovalFile, path)

	writeSubagentApprovalSidecar(sdk.ChatCompletionMessageToolCall{
		Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Bash", Arguments: `{"command":"rm -rf x"}`},
	})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected sidecar to be written: %v", err)
	}
	var af scheddomain.SubagentApprovalFile
	if err := json.Unmarshal(data, &af); err != nil {
		t.Fatalf("unmarshal sidecar: %v", err)
	}
	if !af.Awaiting {
		t.Fatalf("Awaiting should be true")
	}
	if !strings.Contains(af.Summary, "Bash") {
		t.Fatalf("summary should name the pending tool, got %q", af.Summary)
	}

	clearSubagentApprovalSidecar()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("clear should remove the sidecar, stat err = %v", err)
	}
}

// Outside an interactive subagent (env unset) the hooks are a no-op.
func TestSubagentApprovalSidecar_NoopWithoutEnv(t *testing.T) {
	t.Setenv(scheddomain.EnvSubagentApprovalFile, "")
	writeSubagentApprovalSidecar(sdk.ChatCompletionMessageToolCall{
		Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Read"},
	})
	clearSubagentApprovalSidecar()
}
