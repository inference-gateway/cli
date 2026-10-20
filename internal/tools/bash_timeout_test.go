package tools

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// A timed-out command used to block the tool until its last descendant let go
// of the output pipes: a child surviving the kill of bash kept a 120 s timeout
// open for as long as it ran.
func TestBashTool_TimeoutEndsCallWhenChildKeepsPipesOpen(t *testing.T) {
	cfg := &config.Config{Tools: config.ToolsConfig{Enabled: true}, Prompts: *config.DefaultPromptsConfig()}
	cfg.Tools.Bash.Timeout = 1
	tool := NewBashTool(cfg, nil)
	ctx := agentdomain.WithToolApproved(context.Background())
	marker := "infer-bash-timeout-test-" + t.Name()

	start := time.Now()
	result, err := tool.Execute(ctx, map[string]any{"command": ": " + marker + "; (sleep 30) & echo started; wait"})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Execute returned an error: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Execute took %s for a 1 s timeout", elapsed)
	}
	if result.Success {
		t.Fatal("a timed-out command must not be reported as success")
	}
	bash, ok := result.Data.(*agentdomain.BashToolResult)
	if !ok {
		t.Fatalf("unexpected result data %T", result.Data)
	}
	if !strings.Contains(bash.Error, "timed out") {
		t.Fatalf("error %q does not say the command timed out", bash.Error)
	}
	if out, _ := exec.Command("pgrep", "-f", marker).Output(); len(out) > 0 {
		t.Fatalf("the command's child survived the timeout: pids %s", strings.TrimSpace(string(out)))
	}
}
