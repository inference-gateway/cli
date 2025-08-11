package internal

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbletea"
)

func TestNewChatInputModel(t *testing.T) {
	model := NewChatInputModel()

	if model == nil {
		t.Fatal("NewChatInputModel() returned nil")
	}

	if len(model.textarea) != 1 || model.textarea[0] != "" {
		t.Errorf("Expected textarea to be initialized with empty string, got %v", model.textarea)
	}

	if model.cursor != 0 {
		t.Errorf("Expected cursor to be 0, got %d", model.cursor)
	}

	if model.lineIndex != 0 {
		t.Errorf("Expected lineIndex to be 0, got %d", model.lineIndex)
	}

	if model.inputSubmitted {
		t.Errorf("Expected inputSubmitted to be false initially")
	}

	if model.cancelled {
		t.Errorf("Expected cancelled to be false initially")
	}

	if model.quit {
		t.Errorf("Expected quit to be false initially")
	}

	if model.approvalPending {
		t.Errorf("Expected approvalPending to be false initially")
	}
}

func TestChatInputModel_UpdateHistory(t *testing.T) {
	model := NewChatInputModel()
	testHistory := []string{"User: Hello", "Assistant: Hi there!"}

	_, cmd := model.Update(UpdateHistoryMsg{History: testHistory})

	if cmd != nil {
		t.Errorf("Expected no command from UpdateHistoryMsg, got %v", cmd)
	}

	if !equalStringSlices(model.chatHistory, testHistory) {
		t.Errorf("Expected chatHistory to be %v, got %v", testHistory, model.chatHistory)
	}
}

func TestChatInputModel_SetStatus(t *testing.T) {
	model := NewChatInputModel()

	tests := []struct {
		name          string
		message       string
		spinner       bool
		expectSpinner bool
		expectTimer   bool
		expectCommand bool
	}{
		{
			name:          "status without spinner",
			message:       "Ready",
			spinner:       false,
			expectSpinner: false,
			expectTimer:   false,
			expectCommand: false,
		},
		{
			name:          "status with spinner",
			message:       "Processing...",
			spinner:       true,
			expectSpinner: true,
			expectTimer:   true,
			expectCommand: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, cmd := model.Update(SetStatusMsg{Message: tt.message, Spinner: tt.spinner})

			if model.statusMessage != tt.message {
				t.Errorf("Expected statusMessage to be %q, got %q", tt.message, model.statusMessage)
			}

			if model.showSpinner != tt.expectSpinner {
				t.Errorf("Expected showSpinner to be %v, got %v", tt.expectSpinner, model.showSpinner)
			}

			if model.showTimer != tt.expectTimer {
				t.Errorf("Expected showTimer to be %v, got %v", tt.expectTimer, model.showTimer)
			}

			if (cmd != nil) != tt.expectCommand {
				t.Errorf("Expected command existence to be %v, got %v", tt.expectCommand, cmd != nil)
			}
		})
	}
}

func TestChatInputModel_KeyHandling(t *testing.T) {
	tests := []struct {
		name            string
		initialText     string
		initialCursor   int
		key             string
		expectedText    string
		expectedCursor  int
		expectSubmitted bool
		expectCancelled bool
		expectQuit      bool
	}{
		{
			name:           "character input",
			initialText:    "",
			initialCursor:  0,
			key:            "h",
			expectedText:   "h",
			expectedCursor: 1,
		},
		{
			name:           "backspace removes character",
			initialText:    "hello",
			initialCursor:  4,
			key:            "backspace",
			expectedText:   "helo",
			expectedCursor: 3,
		},
		{
			name:           "backspace at start does nothing",
			initialText:    "hello",
			initialCursor:  0,
			key:            "backspace",
			expectedText:   "hello",
			expectedCursor: 0,
		},
		{
			name:           "left arrow moves cursor",
			initialText:    "hello",
			initialCursor:  5,
			key:            "left",
			expectedText:   "hello",
			expectedCursor: 4,
		},
		{
			name:           "left arrow at start does nothing",
			initialText:    "hello",
			initialCursor:  0,
			key:            "left",
			expectedText:   "hello",
			expectedCursor: 0,
		},
		{
			name:            "ctrl+d submits input",
			initialText:     "hello",
			initialCursor:   5,
			key:             "ctrl+d",
			expectedText:    "hello",
			expectedCursor:  5,
			expectSubmitted: true,
		},
		{
			name:            "esc cancels generation",
			initialText:     "hello",
			initialCursor:   5,
			key:             "esc",
			expectedText:    "hello",
			expectedCursor:  5,
			expectCancelled: true,
		},
		{
			name:       "ctrl+c quits",
			key:        "ctrl+c",
			expectQuit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := NewChatInputModel()

			if tt.initialText != "" {
				model.textarea[0] = tt.initialText
			}
			model.cursor = tt.initialCursor

			if tt.key == "esc" {
				model.showSpinner = true
			}

			var cmd tea.Cmd
			if tt.key == "backspace" {
				_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyBackspace})
			} else if tt.key == "left" {
				_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyLeft})
			} else if tt.key == "ctrl+d" {
				_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
			} else if tt.key == "esc" {
				_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
			} else if tt.key == "ctrl+c" {
				_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
			} else if len(tt.key) == 1 {
				_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)})
			}

			if tt.expectedText != "" && model.textarea[0] != tt.expectedText {
				t.Errorf("Expected text to be %q, got %q", tt.expectedText, model.textarea[0])
			}

			if model.cursor != tt.expectedCursor {
				t.Errorf("Expected cursor to be %d, got %d", tt.expectedCursor, model.cursor)
			}

			if model.inputSubmitted != tt.expectSubmitted {
				t.Errorf("Expected inputSubmitted to be %v, got %v", tt.expectSubmitted, model.inputSubmitted)
			}

			if model.cancelled != tt.expectCancelled {
				t.Errorf("Expected cancelled to be %v, got %v", tt.expectCancelled, model.cancelled)
			}

			if tt.expectQuit {
				if cmd == nil {
					t.Errorf("Expected quit command, got nil")
				}
				if model.quit != true {
					t.Errorf("Expected quit to be true, got %v", model.quit)
				}
			}
		})
	}
}

func TestChatInputModel_ApprovalFlow(t *testing.T) {
	model := NewChatInputModel()

	_, cmd := model.Update(ApprovalRequestMsg{Command: "rm -rf /"})

	if cmd != nil {
		t.Errorf("Expected no command from ApprovalRequestMsg, got %v", cmd)
	}

	if !model.approvalPending {
		t.Errorf("Expected approvalPending to be true")
	}

	if model.approvalCommand != "rm -rf /" {
		t.Errorf("Expected approvalCommand to be 'rm -rf /', got %q", model.approvalCommand)
	}

	if model.approvalSelected != 0 {
		t.Errorf("Expected approvalSelected to be 0, got %d", model.approvalSelected)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if model.approvalSelected != 1 {
		t.Errorf("Expected approvalSelected to be 1 after down arrow, got %d", model.approvalSelected)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	if model.approvalSelected != 0 {
		t.Errorf("Expected approvalSelected to be 0 after up arrow, got %d", model.approvalSelected)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if model.approvalPending {
		t.Errorf("Expected approvalPending to be false after selection")
	}

	if model.approvalResponse != 1 { // First option is "Allow"
		t.Errorf("Expected approvalResponse to be 1, got %d", model.approvalResponse)
	}
}

func TestChatInputModel_InputOutput(t *testing.T) {
	model := NewChatInputModel()

	// Initially no input
	if model.HasInput() {
		t.Errorf("Expected HasInput() to be false initially")
	}

	if model.GetInput() != "" {
		t.Errorf("Expected GetInput() to return empty string initially")
	}

	model.textarea[0] = "test input"
	model.cursor = len("test input")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlD})

	if !model.HasInput() {
		t.Errorf("Expected HasInput() to be true after submission")
	}

	input := model.GetInput()
	if input != "test input" {
		t.Errorf("Expected GetInput() to return 'test input', got %q", input)
	}

	if model.HasInput() {
		t.Errorf("Expected HasInput() to be false after GetInput() call")
	}

	if model.textarea[0] != "" {
		t.Errorf("Expected textarea to be cleared after GetInput(), got %q", model.textarea[0])
	}

	if model.cursor != 0 {
		t.Errorf("Expected cursor to be reset to 0, got %d", model.cursor)
	}
}

func TestChatInputModel_CancellationFlow(t *testing.T) {
	model := NewChatInputModel()

	if model.IsCancelled() {
		t.Errorf("Expected IsCancelled() to be false initially")
	}

	model.showSpinner = true
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !model.IsCancelled() {
		t.Errorf("Expected IsCancelled() to be true after Esc during spinner")
	}

	model.ResetCancellation()

	if model.IsCancelled() {
		t.Errorf("Expected IsCancelled() to be false after ResetCancellation()")
	}
}

func TestChatInputModel_View(t *testing.T) {
	model := NewChatInputModel()
	model.width = 80
	model.height = 20

	view := model.View()

	if view == "" {
		t.Errorf("Expected non-empty view")
	}

	if !strings.Contains(view, "> │") {
		t.Errorf("Expected view to contain input prompt '> │'")
	}

	if !strings.Contains(view, "Ctrl+D") {
		t.Errorf("Expected view to contain help text about Ctrl+D")
	}

	model.statusMessage = "Test status"
	view = model.View()

	if !strings.Contains(view, "Test status") {
		t.Errorf("Expected view to contain status message")
	}

	model.approvalPending = true
	model.approvalCommand = "test command"
	view = model.View()

	if !strings.Contains(view, "test command") {
		t.Errorf("Expected view to contain approval command")
	}

	if !strings.Contains(view, "Yes - Execute") {
		t.Errorf("Expected view to contain approval options")
	}
}

func TestChatInputModel_WindowResize(t *testing.T) {
	model := NewChatInputModel()

	_, cmd := model.Update(tea.WindowSizeMsg{Width: 120, Height: 30})

	if cmd != nil {
		t.Errorf("Expected no command from WindowSizeMsg, got %v", cmd)
	}

	if model.width != 120 {
		t.Errorf("Expected width to be 120, got %d", model.width)
	}

	if model.height != 30 {
		t.Errorf("Expected height to be 30, got %d", model.height)
	}
}

func TestChatInputModel_SpinnerTick(t *testing.T) {
	model := NewChatInputModel()
	model.showSpinner = true

	_, cmd := model.Update(SpinnerTick{})

	if cmd == nil {
		t.Errorf("Expected command to continue spinner ticking")
	}

	if model.spinnerFrame == 0 {
		t.Errorf("Expected spinnerFrame to be incremented")
	}

	model.showSpinner = false
	_, cmd = model.Update(SpinnerTick{})

	if cmd != nil {
		t.Errorf("Expected no command when spinner is off, got %v", cmd)
	}
}

// Helper function to compare string slices
func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
