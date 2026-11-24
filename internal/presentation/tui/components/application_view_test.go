package components

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"
	tuimocks "github.com/inference-gateway/cli/tests/mocks/tui"

	lipgloss "charm.land/lipgloss/v2"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*m")

// plain drops ANSI escapes. Test content is single-width, so rune count then
// equals display columns.
func plain(s string) string { return ansiEscape.ReplaceAllString(s, "") }

func visibleWidth(s string) int { return len([]rune(plain(s))) }

func modeIndicatorFor(mode agentdomain.AgentMode) *ModeIndicator {
	indicator := NewModeIndicator(styles.NewProvider(styles.NewThemeProvider()))
	state := &agentdomainmocks.FakeAgentModeState{}
	state.GetAgentModeReturns(mode)
	indicator.SetStateManager(state)
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
			status := &tuimocks.FakeStatusComponent{}
			status.RenderReturns(tt.status)
			rows := renderer.appendStatusRow(nil, status, indicator, width, 1)
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

// TestSubagentRowsRenderBelowTheStatusBar pins the layout contract: the live
// sub-agent elapsed rows sit below the indicator row that carries the versions,
// never between the composer and it.
func TestSubagentRowsRenderBelowTheStatusBar(t *testing.T) {
	const width = 120
	styleProvider := styles.NewProvider(styles.NewThemeProvider())
	renderer := NewApplicationViewRenderer(styleProvider)

	statusBar := NewInputStatusBar(styleProvider)
	statusBar.SetWidth(width)
	statusBar.SetVersionInfo(tui.VersionInfo{Version: "0.1.0", GatewayVersion: "0.2.0"})

	list := newList(listOpts{
		jobs:      []scheddomain.TrackedJob{subagentJob("reviewer", scheddomain.JobRunning, time.Now().Add(-3*time.Second), nil)},
		linger:    5,
		indicator: true,
	})
	list.SetWidth(width)

	rows := renderer.assembleComponents(
		ChatInterfaceData{Width: width, Height: 24},
		"", "", "> ",
		nil, &tuimocks.FakeStatusComponent{}, nil, &tuimocks.FakeInputComponent{}, statusBar, list,
		nil, &tuimocks.FakeHelpBarComponent{}, nil, nil, nil, nil, nil, nil,
		width, 0,
	)

	frame := plain(strings.Join(rows, "\n"))
	versionRow := strings.Index(frame, "cli v0.1.0")
	subagentRow := strings.Index(frame, "reviewer")
	if versionRow < 0 || subagentRow < 0 {
		t.Fatalf("expected both the version row and the sub-agent row, got %q", frame)
	}
	if subagentRow < versionRow {
		t.Errorf("the sub-agent elapsed row must render below the version row, got %q", frame)
	}
}

// TestLayoutFillsTheTerminalHeight pins that the frame is exactly as tall as the
// terminal: the transcript takes every row the measured chrome leaves, so the
// composer sits on the bottom row and a jobs list shrinks the transcript by its
// own line count instead of pushing the frame past the terminal.
func TestLayoutFillsTheTerminalHeight(t *testing.T) {
	const width = 120
	styleProvider := styles.NewProvider(styles.NewThemeProvider())

	jobs := make([]scheddomain.TrackedJob, 0, 5)
	for i := range 5 {
		jobs = append(jobs, subagentJob(fmt.Sprintf("w%d", i), scheddomain.JobRunning, time.Now().Add(-time.Duration(5-i)*time.Second), nil))
	}

	for _, height := range []int{29, 40} {
		for _, withList := range []bool{false, true} {
			t.Run(fmt.Sprintf("height %d list %t", height, withList), func(t *testing.T) {
				renderer := NewApplicationViewRenderer(styleProvider)
				data := ChatInterfaceData{Width: width, Height: height}

				conversation := &tuimocks.FakeConversationRenderer{}
				conversation.SetHeightCalls(func(h int) { conversation.RenderReturns(strings.Repeat("\n", h-1)) })
				input := &tuimocks.FakeInputComponent{}
				input.RenderReturns("╭──╮\n│ > │\n╰──╯")
				status := &tuimocks.FakeStatusComponent{}
				status.RenderReturns(" Response complete")
				statusBar := NewInputStatusBar(styleProvider)
				helpBar := &tuimocks.FakeHelpBarComponent{}

				var list *SubagentList
				if withList {
					list = newList(listOpts{jobs: jobs, linger: 5, indicator: true})
					list.SetWidth(width)
				}

				renderer.Layout(data, conversation, input, nil, statusBar, status, nil, helpBar, nil, nil, nil, nil, nil, nil, list)
				frame := renderer.RenderChatInterface(data, conversation, input, nil, statusBar, list, status, nil, helpBar, nil, nil, nil, nil, nil, nil)

				if got := strings.Count(frame, "\n") + 1; got != height {
					t.Fatalf("frame is %d lines, want %d:\n%s", got, height, plain(frame))
				}
				if !withList {
					return
				}
				listLines := strings.Count(list.Render(), "\n") + 1
				bare := NewApplicationViewRenderer(styleProvider)
				bare.Layout(data, conversation, input, nil, statusBar, status, nil, helpBar, nil, nil, nil, nil, nil, nil, nil)
				if got := bare.ConversationHeight() - renderer.ConversationHeight(); got != listLines {
					t.Errorf("transcript shrank by %d lines, want the list's %d", got, listLines)
				}
			})
		}
	}
}

// TestStatusBarTopIsTheStatusBarsRow pins the row a click is mapped from: the
// rendered frame's line holding the indicators is the one StatusBarTop names.
func TestStatusBarTopIsTheStatusBarsRow(t *testing.T) {
	const width, height = 120, 30
	styleProvider := styles.NewProvider(styles.NewThemeProvider())
	renderer := NewApplicationViewRenderer(styleProvider)
	data := ChatInterfaceData{Width: width, Height: height}
	conversation := &tuimocks.FakeConversationRenderer{}
	conversation.SetHeightCalls(func(h int) { conversation.RenderReturns(strings.Repeat("\n", h-1)) })
	input := &tuimocks.FakeInputComponent{}
	input.RenderReturns("╭──╮\n│ > │\n╰──╯")
	modelService := &convmocks.FakeModelService{}
	modelService.GetCurrentModelReturns("test-model")
	statusBar := NewInputStatusBar(styleProvider)
	statusBar.SetModelService(modelService)
	statusBar.SetConfig(config.DefaultConfig())
	statusBar.EnableRooms()
	status := &tuimocks.FakeStatusComponent{}
	helpBar := &tuimocks.FakeHelpBarComponent{}

	renderer.Layout(data, conversation, input, nil, statusBar, status, nil, helpBar, nil, nil, nil, nil, nil, nil, nil)
	frame := plain(renderer.RenderChatInterface(data, conversation, input, nil, statusBar, nil, status, nil, helpBar, nil, nil, nil, nil, nil, nil))

	lines := strings.Split(frame, "\n")
	row := slices.IndexFunc(lines, func(line string) bool { return strings.Contains(line, "Room: -") })
	if row < 0 || row != renderer.StatusBarTop() {
		t.Fatalf("the indicators are on row %d, StatusBarTop says %d:\n%s", row, renderer.StatusBarTop(), frame)
	}
	column := lipgloss.Width(lines[row][:strings.Index(lines[row], "Room: -")])
	action, ok := statusBar.ActionAt(column, row-renderer.StatusBarTop())
	if !ok || action != tui.StatusIndicatorActionRoomSelection {
		t.Fatalf("a click on the room indicator hit %v (%t), want the room selection", action, ok)
	}
}
