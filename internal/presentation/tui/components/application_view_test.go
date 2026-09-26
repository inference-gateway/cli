package components

import (
	"regexp"
	"strings"
	"testing"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
)

// stubModeState is the three-method agent-mode surface the indicator reads.
type stubModeState struct{ mode agentdomain.AgentMode }

func (s stubModeState) GetAgentMode() agentdomain.AgentMode   { return s.mode }
func (s stubModeState) SetAgentMode(_ agentdomain.AgentMode)  {}
func (s stubModeState) CycleAgentMode() agentdomain.AgentMode { return s.mode }

// stubStatusView is the status surface the row builder reads; the rest of the
// interface is unused by it.
type stubStatusView struct {
	tui.StatusComponent
	rendered string
}

func (s stubStatusView) Render() string { return s.rendered }

var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*m")

// plain drops ANSI escapes. Test content is single-width, so rune count then
// equals display columns.
func plain(s string) string { return ansiEscape.ReplaceAllString(s, "") }

func visibleWidth(s string) int { return len([]rune(plain(s))) }

func modeIndicatorFor(mode agentdomain.AgentMode) *ModeIndicator {
	indicator := NewModeIndicator(styles.NewProvider(styles.NewThemeProvider()))
	indicator.SetStateManager(stubModeState{mode: mode})
	return indicator
}

// TestStatusRowSharesOneLineWithMode pins the layout contract: the agent mode
// label rides the status row instead of claiming a row of its own, no line ever
// grows past the width the row was given, and a status too long to share the
// row is truncated rather than pushing the mode label off it.
func TestStatusRowSharesOneLineWithMode(t *testing.T) {
	const width = 80
	renderer := NewApplicationViewRenderer(styles.NewProvider(styles.NewThemeProvider()))

	tests := []struct {
		name          string
		status        string
		judgeModel    string
		mode          agentdomain.AgentMode
		wantLines     int
		wantModeText  string
		wantTruncated bool
	}{
		{
			name:         "spinner and mode share the row",
			status:       " ⠋ Processing... (1.2s)",
			mode:         agentdomain.AgentModeAutoWithJudge,
			wantLines:    1,
			wantModeText: "▸ AUTO+JUDGE",
		},
		{
			name:         "mode without a status stays right aligned",
			status:       "",
			mode:         agentdomain.AgentModePlan,
			wantLines:    1,
			wantModeText: "▶ PLAN",
		},
		{
			name:      "status without a mode is returned untouched",
			status:    " ⠋ Processing... (1.2s)",
			mode:      agentdomain.AgentModeStandard,
			wantLines: 1,
		},
		{
			name:         "wrapped status keeps its extra lines",
			status:       " ⠋ first\nsecond",
			mode:         agentdomain.AgentModeAutoAccept,
			wantLines:    2,
			wantModeText: "▸ AUTO",
		},
		{
			name:          "long status is truncated to make room for the mode",
			status:        " ⠋ Processing... a very long status message that would definitely overflow the row budget (1.2s)",
			judgeModel:    "ollama_cloud/glm-5.3-flash",
			mode:          agentdomain.AgentModeAutoWithJudge,
			wantLines:     1,
			wantModeText:  "▸ AUTO+JUDGE · ollama_cloud/glm-5.3-flash",
			wantTruncated: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			indicator := modeIndicatorFor(tt.mode)
			if tt.judgeModel != "" {
				indicator.SetJudgeModelFn(func() string { return tt.judgeModel })
			}
			rows := renderer.appendStatusRow(nil, stubStatusView{rendered: tt.status}, indicator, width, 1)
			if len(rows) != 1 {
				t.Fatalf("expected exactly one row, got %d", len(rows))
			}

			lines := strings.Split(rows[0], "\n")
			if len(lines) != tt.wantLines {
				t.Fatalf("row rendered %d lines, want %d: %q", len(lines), tt.wantLines, plain(rows[0]))
			}

			for _, line := range lines {
				if got := visibleWidth(line); got > width-4 {
					t.Errorf("line %q is %d columns, want at most %d", plain(line), got, width-4)
				}
			}

			if tt.wantModeText == "" {
				if got := plain(rows[0]); got != tt.status {
					t.Errorf("row = %q, want the status untouched: %q", got, tt.status)
				}
				return
			}

			if got := strings.TrimRight(plain(lines[0]), " "); !strings.HasSuffix(got, tt.wantModeText) {
				t.Errorf("mode %q must end the first line, got %q", tt.wantModeText, got)
			}

			firstStatusLine := strings.Split(tt.status, "\n")[0]
			if tt.wantTruncated {
				if got := visibleWidth(lines[0]); got >= visibleWidth(firstStatusLine) {
					t.Errorf("first line is %d columns, want it truncated below the input's %d", got, visibleWidth(firstStatusLine))
				}
			} else if tt.status != "" && !strings.Contains(plain(lines[0]), firstStatusLine) {
				t.Errorf("status %q must share the mode's line, got %q", tt.status, plain(lines[0]))
			}
		})
	}
}
