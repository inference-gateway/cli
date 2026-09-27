package computer

import (
	"testing"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func TestCallApproval(t *testing.T) {
	yes, no := true, false
	click := map[string]any{"action": "click", "x": 1.0, "y": 1.0}
	screenshot := map[string]any{"action": "screenshot"}
	tests := []struct {
		name     string
		approval string
		record   *bool
		mode     agentdomain.AgentMode
		tool     string
		args     map[string]any
		want     bool
	}{
		{name: "never bypasses destructive action", approval: config.ComputerUseApprovalNever, tool: ToolComputer, args: click},
		{name: "empty bypasses destructive action", tool: ToolComputer, args: click},
		{name: "empty bypasses GetLatestFrame", tool: ToolGetLatestFrame},
		{name: "empty bypasses RecordStop", tool: ToolRecordStop},
		{name: "destructive gates click", approval: config.ComputerUseApprovalDestructive, tool: ToolComputer, args: click, want: true},
		{name: "destructive gates type", approval: config.ComputerUseApprovalDestructive, tool: ToolComputer, args: map[string]any{"action": "type", "text": "hi"}, want: true},
		{name: "destructive bypasses screenshot", approval: config.ComputerUseApprovalDestructive, tool: ToolComputer, args: screenshot},
		{name: "destructive bypasses cursor", approval: config.ComputerUseApprovalDestructive, tool: ToolComputer, args: map[string]any{"action": "cursor"}},
		{name: "destructive bypasses accessibility", approval: config.ComputerUseApprovalDestructive, tool: ToolComputer, args: map[string]any{"action": "accessibility"}},
		{name: "destructive gates accessibility press", approval: config.ComputerUseApprovalDestructive, tool: ToolComputer, args: map[string]any{"action": "press", "label": "Save"}, want: true},
		{name: "destructive gates malformed arguments", approval: config.ComputerUseApprovalDestructive, tool: ToolComputer, want: true},
		{name: "destructive bypasses GetLatestFrame", approval: config.ComputerUseApprovalDestructive, tool: ToolGetLatestFrame},
		{name: "destructive bypasses RecordStop", approval: config.ComputerUseApprovalDestructive, tool: ToolRecordStop},
		{name: "always gates screenshot", approval: config.ComputerUseApprovalAlways, tool: ToolComputer, args: screenshot, want: true},
		{name: "always gates RecordStart", approval: config.ComputerUseApprovalAlways, record: &no, tool: ToolRecordStart, want: true},
		{name: "unknown value fails closed", approval: "alway", tool: ToolComputer, args: screenshot, want: true},
		{name: "RecordStart requires approval by default", tool: ToolRecordStart, want: true},
		{name: "RecordStart keeps approval when required", record: &yes, tool: ToolRecordStart, want: true},
		{name: "RecordStart skips approval when not required", record: &no, tool: ToolRecordStart},
		{name: "auto-accept bypasses RecordStart approval", mode: agentdomain.AgentModeAutoAccept, tool: ToolRecordStart},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.ComputerUse.Enabled = true
			cfg.ComputerUse.Recording.Enabled = true
			cfg.ComputerUse.Approval = tt.approval
			cfg.ComputerUse.Recording.RequireApproval = tt.record
			approver, ok := NewTools(cfg, nil, nil, nil)[tt.tool].(agentdomain.CallApprover)
			if !ok {
				t.Fatalf("%s does not approve per call", tt.tool)
			}
			if got := approver.RequiresApproval(tt.args, tt.mode); got != tt.want {
				t.Errorf("RequiresApproval(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestRecordToolsNeedSession(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.ComputerUse.Recording.Enabled = true
	for _, name := range []string{ToolRecordStart, ToolRecordStop} {
		if tool, ok := NewTools(cfg, nil, nil, nil)[name].(agentdomain.SessionTool); !ok || !tool.NeedsSession() {
			t.Errorf("%s must be finalized by the session that started it", name)
		}
	}
}
