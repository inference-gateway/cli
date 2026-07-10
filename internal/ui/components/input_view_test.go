package components

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"

	tea "charm.land/bubbletea/v2"

	config "github.com/inference-gateway/cli/config"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
	uimocks "github.com/inference-gateway/cli/tests/mocks/ui"

	domain "github.com/inference-gateway/cli/internal/domain"
	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
	history "github.com/inference-gateway/cli/internal/ui/history"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// createMockModelService creates a fake model service with default test values
func createMockModelService() *domainmocks.FakeModelService {
	fake := &domainmocks.FakeModelService{}
	fake.ListModelsReturns([]string{"test-model"}, nil)
	fake.GetCurrentModelReturns("test-model")
	fake.IsModelAvailableReturns(true)
	fake.ValidateModelReturns(nil)
	fake.IsVisionModelReturns(false)
	return fake
}

// createInputViewWithTheme creates an InputView with isolated memory-only history for testing
func createInputViewWithTheme(modelService domain.ModelService) *InputView {
	ta := newInputTextarea("Type your message...")

	iv := &InputView{
		ta:               ta,
		placeholder:      "Type your message...",
		width:            80,
		height:           5,
		modelService:     modelService,
		historyManager:   history.NewMemoryOnlyHistoryManager(5),
		themeService:     nil,
		imageAttachments: []domain.ImageAttachment{},
	}

	fakeTheme := &uimocks.FakeTheme{}
	fakeTheme.GetDimColorReturns("#888888")
	fakeTheme.GetStatusColorReturns("#00ff00")
	fakeTheme.GetAccentColorReturns("#00ffff")
	fakeTheme.GetBorderColorReturns("#555555")

	fakeThemeService := &domainmocks.FakeThemeService{}
	fakeThemeService.GetCurrentThemeReturns(fakeTheme)

	iv.themeService = fakeThemeService
	iv.styleProvider = styles.NewProvider(fakeThemeService)

	return iv
}

// NewInputViewWithName must map the memory-only sentinel to a manager with no backing
// file (unlabeled subagents), and a real name to <configDir>/history/history-<name>.
func TestNewInputViewWithName_HistorySelection(t *testing.T) {
	ms := createMockModelService()

	iv := NewInputViewWithName(ms, "cfgdir", domain.SubagentHistoryMemoryOnly)
	if got := iv.historyManager.GetShellHistoryFile(); got != "" {
		t.Errorf("memory-only sentinel must have no history file, got %q", got)
	}

	iv = NewInputViewWithName(ms, "cfgdir", "refactor")
	want := filepath.Join("cfgdir", "history", "history-refactor")
	if got := iv.historyManager.GetShellHistoryFile(); got != want {
		t.Errorf("named subagent history: want %q, got %q", want, got)
	}
}

func TestNewInputView(t *testing.T) {
	mockModelService := createMockModelService()
	iv := NewInputView(mockModelService)

	if iv.GetInput() != "" {
		t.Errorf("Expected empty text, got '%s'", iv.GetInput())
	}

	if iv.GetCursor() != 0 {
		t.Errorf("Expected cursor at 0, got %d", iv.GetCursor())
	}

	if iv.width != 80 {
		t.Errorf("Expected default width 80, got %d", iv.width)
	}

	if iv.height != 5 {
		t.Errorf("Expected default height 5, got %d", iv.height)
	}

	if iv.modelService != mockModelService {
		t.Error("Expected model service to be set")
	}

	if iv.historyManager == nil {
		t.Error("Expected history manager to be initialized")
	}
}

func TestInputView_GetInput(t *testing.T) {
	mockModelService := createMockModelService()
	iv := NewInputView(mockModelService)

	testText := "Hello, world!"
	iv.SetText(testText)

	if iv.GetInput() != testText {
		t.Errorf("Expected GetInput to return '%s', got '%s'", testText, iv.GetInput())
	}
}

func TestInputView_ClearInput(t *testing.T) {
	mockModelService := createMockModelService()
	iv := NewInputView(mockModelService)

	iv.SetText("Some text")
	iv.SetCursor(5)

	iv.ClearInput()

	if iv.GetInput() != "" {
		t.Errorf("Expected empty text after clear, got '%s'", iv.GetInput())
	}

	if iv.GetCursor() != 0 {
		t.Errorf("Expected cursor at 0 after clear, got %d", iv.GetCursor())
	}
}

func TestInputView_AddImageAttachmentTokenHasNoIssueRef(t *testing.T) {
	iv := NewInputView(createMockModelService())

	iv.AddImageAttachment(domain.ImageAttachment{})
	iv.AddImageAttachment(domain.ImageAttachment{})

	input := iv.GetInput()

	if strings.Contains(input, "#") {
		t.Errorf("image placeholder must not contain '#'; got %q", input)
	}
	if !strings.Contains(input, "[Image 1]") || !strings.Contains(input, "[Image 2]") {
		t.Errorf("expected sequential [Image N] tokens, got %q", input)
	}
	if got := len(iv.GetImageAttachments()); got != 2 {
		t.Errorf("expected 2 tracked attachments, got %d", got)
	}
}

func TestInputView_SetPlaceholder(t *testing.T) {
	mockModelService := createMockModelService()
	iv := NewInputView(mockModelService)

	testPlaceholder := "Enter your message..."
	iv.SetPlaceholder(testPlaceholder)

	if iv.placeholder != testPlaceholder {
		t.Errorf("Expected placeholder '%s', got '%s'", testPlaceholder, iv.placeholder)
	}
}

func TestInputView_GetCursor(t *testing.T) {
	mockModelService := createMockModelService()
	iv := NewInputView(mockModelService)

	iv.SetText("Hello World")
	iv.SetCursor(5)

	if iv.GetCursor() != 5 {
		t.Errorf("Expected cursor position 5, got %d", iv.GetCursor())
	}
}

func TestInputView_SetCursor(t *testing.T) {
	mockModelService := createMockModelService()
	iv := NewInputView(mockModelService)

	iv.SetCursor(15)
	if iv.GetCursor() != 0 {
		t.Errorf("Expected cursor to remain at 0 for invalid position, got %d", iv.GetCursor())
	}

	iv.SetText("Hello World")
	iv.SetCursor(5)
	if iv.GetCursor() != 5 {
		t.Errorf("Expected cursor position 5, got %d", iv.GetCursor())
	}
}

func TestInputView_SetText(t *testing.T) {
	mockModelService := createMockModelService()
	iv := NewInputView(mockModelService)

	testText := "New text content"
	iv.SetText(testText)

	if iv.GetInput() != testText {
		t.Errorf("Expected text '%s', got '%s'", testText, iv.GetInput())
	}

	if iv.GetCursor() != 16 {
		t.Errorf("Expected cursor at end of text (16), got %d", iv.GetCursor())
	}
}

func TestInputView_SetWidth(t *testing.T) {
	mockModelService := createMockModelService()
	iv := NewInputView(mockModelService)

	iv.SetWidth(120)

	if iv.width != 120 {
		t.Errorf("Expected width 120, got %d", iv.width)
	}
}

func TestInputView_SetHeight(t *testing.T) {
	mockModelService := createMockModelService()
	iv := NewInputView(mockModelService)

	iv.SetHeight(8)

	if iv.height != 8 {
		t.Errorf("Expected height 8, got %d", iv.height)
	}
}

func TestInputView_Render(t *testing.T) {
	mockModelService := createMockModelService()
	iv := createInputViewWithTheme(mockModelService)

	output := iv.Render()
	if output == "" {
		t.Error("Expected non-empty render output")
	}

	iv.SetText("Hello")
	output = iv.Render()
	if output == "" {
		t.Error("Expected non-empty render output with text")
	}
}

func TestInputView_RenderUsesCompactTextareaHeight(t *testing.T) {
	mockModelService := createMockModelService()
	iv := createInputViewWithTheme(mockModelService)
	iv.SetWidth(80)
	iv.SetHeight(4)

	iv.SetText("say hello")
	if got := iv.textareaContentHeight(); got != 1 {
		t.Fatalf("expected short input to use one textarea row, got %d", got)
	}

	output := stripANSI(iv.Render())
	if lines := strings.Split(output, "\n"); len(lines) != 3 {
		t.Fatalf("expected one content row plus border, got %d lines:\n%s", len(lines), output)
	}

	iv.SetText("say\nhello")
	if got := iv.textareaContentHeight(); got != 2 {
		t.Fatalf("expected two explicit lines to use two textarea rows, got %d", got)
	}
}

func TestInputView_TextareaEditingKeyCompatibility(t *testing.T) {
	tests := []struct {
		name   string
		start  string
		cursor int
		key    tea.KeyPressMsg
		want   string
		wantAt int
	}{
		{
			name:   "ctrl+j inserts newline",
			start:  "hello",
			cursor: 5,
			key:    tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl},
			want:   "hello\n",
			wantAt: 6,
		},
		{
			name:   "alt+enter inserts newline",
			start:  "hello",
			cursor: 5,
			key:    tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt},
			want:   "hello\n",
			wantAt: 6,
		},
		{
			name:   "ctrl+backspace deletes previous word",
			start:  "hello world",
			cursor: 11,
			key:    tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModCtrl},
			want:   "hello ",
			wantAt: 6,
		},
		{
			name:   "ctrl+a moves to input beginning",
			start:  "hello\nworld",
			cursor: 11,
			key:    tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl},
			want:   "hello\nworld",
			wantAt: 0,
		},
		{
			name:   "ctrl+e moves to input end",
			start:  "hello\nworld",
			cursor: 0,
			key:    tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl},
			want:   "hello\nworld",
			wantAt: 11,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iv := createInputViewWithTheme(createMockModelService())
			iv.SetText(tt.start)
			iv.SetCursor(tt.cursor)
			_, _ = iv.Update(tea.FocusMsg{})

			_, _ = iv.Update(tt.key)

			if got := iv.GetInput(); got != tt.want {
				t.Fatalf("input = %q, want %q", got, tt.want)
			}
			if got := iv.GetCursor(); got != tt.wantAt {
				t.Fatalf("cursor = %d, want %d", got, tt.wantAt)
			}
		})
	}
}

func TestInputView_RenderHighlightsRegisteredShortcut(t *testing.T) {
	iv := createInputViewWithTheme(createMockModelService())
	registry := shortcuts.NewRegistry()
	registry.Register(shortcuts.NewInitShortcut(config.DefaultConfig()))
	iv.SetShortcutRegistry(registry)
	iv.focused = false
	iv.SetText("/init")

	out := iv.renderTextWithCursor()
	if got := stripANSI(out); got != "/init" {
		t.Fatalf("plain rendered text = %q, want /init", got)
	}
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("expected rendered shortcut to contain ANSI highlighting, got %q", out)
	}
}

func TestInputView_CanHandle(t *testing.T) {
	mockModelService := createMockModelService()
	iv := NewInputView(mockModelService)

	charKey := tea.KeyPressMsg{Text: "a"}
	if !iv.CanHandle(charKey) {
		t.Error("Expected CanHandle to return true for character input")
	}

	backspaceKey := tea.KeyPressMsg{Code: tea.KeyBackspace}
	if !iv.CanHandle(backspaceKey) {
		t.Error("Expected CanHandle to return true for backspace")
	}

	leftKey := tea.KeyPressMsg{Code: tea.KeyLeft}
	if !iv.CanHandle(leftKey) {
		t.Error("Expected CanHandle to return true for left arrow")
	}

	rightKey := tea.KeyPressMsg{Code: tea.KeyRight}
	if !iv.CanHandle(rightKey) {
		t.Error("Expected CanHandle to return true for right arrow")
	}
}

func TestInputView_History(t *testing.T) {
	mockModelService := createMockModelService()
	iv := NewInputView(mockModelService)

	if iv.historyManager == nil {
		t.Error("Expected history manager to be initialized")
	}
}

func TestInputView_BashModeBorderColor(t *testing.T) {
	mockModelService := createMockModelService()
	iv := createInputViewWithTheme(mockModelService)

	iv.SetText("normal text")
	normalOutput := iv.Render()
	if normalOutput == "" {
		t.Error("Expected non-empty render output for normal text")
	}

	iv.SetText("!")
	bashOutput := iv.Render()
	if bashOutput == "" {
		t.Error("Expected non-empty render output for bash mode")
	}

	iv.SetText("!!")
	toolsOutput := iv.Render()
	if toolsOutput == "" {
		t.Error("Expected non-empty render output for tools mode")
	}
}

func TestInputView_HistorySuggestions_SingleMatch(t *testing.T) {
	mockModelService := createMockModelService()
	iv := createInputViewWithTheme(mockModelService)

	require.NoError(t, iv.historyManager.AddToHistory("create a pull request"))
	require.NoError(t, iv.historyManager.AddToHistory("list files"))

	iv.SetText("cre")
	iv.SetCursor(len(iv.GetInput()))

	iv.Render()

	if !iv.HasHistorySuggestion() {
		t.Error("Expected history suggestion to be available")
	}

	if iv.historySuggestion != "ate a pull request" {
		t.Errorf("Expected suggestion 'ate a pull request', got '%s'", iv.historySuggestion)
	}

	if len(iv.historySuggestions) != 1 {
		t.Errorf("Expected 1 matching suggestion, got %d", len(iv.historySuggestions))
	}
}

func TestInputView_HistorySuggestions_MultipleMatches(t *testing.T) {
	mockModelService := createMockModelService()
	iv := createInputViewWithTheme(mockModelService)

	require.NoError(t, iv.historyManager.AddToHistory("create a pull request"))
	require.NoError(t, iv.historyManager.AddToHistory("create a new branch"))
	require.NoError(t, iv.historyManager.AddToHistory("create tests"))

	iv.SetText("create")
	iv.SetCursor(len(iv.GetInput()))

	iv.Render()

	if len(iv.historySuggestions) != 3 {
		t.Errorf("Expected 3 matching suggestions, got %d", len(iv.historySuggestions))
	}

	if iv.historySuggestion != " a pull request" {
		t.Errorf("Expected suggestion ' a pull request', got '%s'", iv.historySuggestion)
	}
}

func TestInputView_HistorySuggestions_CycleThrough(t *testing.T) {
	mockModelService := createMockModelService()
	iv := createInputViewWithTheme(mockModelService)

	require.NoError(t, iv.historyManager.AddToHistory("create a pull request"))
	require.NoError(t, iv.historyManager.AddToHistory("create a new branch"))

	iv.SetText("create")
	iv.SetCursor(len(iv.GetInput()))
	iv.Render()

	firstSuggestion := iv.historySuggestion

	iv.cycleHistorySuggestion()
	secondSuggestion := iv.historySuggestion

	if firstSuggestion == secondSuggestion {
		t.Error("Expected different suggestion after cycling")
	}

	iv.cycleHistorySuggestion()
	if iv.historySuggestion != firstSuggestion {
		t.Errorf("Expected to cycle back to first suggestion '%s', got '%s'", firstSuggestion, iv.historySuggestion)
	}
}

func TestInputView_HistorySuggestions_AcceptSuggestion(t *testing.T) {
	mockModelService := createMockModelService()
	iv := createInputViewWithTheme(mockModelService)

	require.NoError(t, iv.historyManager.AddToHistory("create a pull request"))

	iv.SetText("cre")
	iv.SetCursor(len(iv.GetInput()))
	iv.Render()

	accepted := iv.AcceptHistorySuggestion()

	if !accepted {
		t.Error("Expected AcceptHistorySuggestion to return true")
	}

	if iv.GetInput() != "create a pull request" {
		t.Errorf("Expected text to be 'create a pull request', got '%s'", iv.GetInput())
	}

	if iv.GetCursor() != len(iv.GetInput()) {
		t.Errorf("Expected cursor to be at end (%d), got %d", len(iv.GetInput()), iv.GetCursor())
	}

	if iv.HasHistorySuggestion() {
		t.Error("Expected suggestion to be cleared after acceptance")
	}
}

func TestInputView_HistorySuggestions_NoMatchWhenEmpty(t *testing.T) {
	mockModelService := createMockModelService()
	iv := createInputViewWithTheme(mockModelService)

	require.NoError(t, iv.historyManager.AddToHistory("create a pull request"))

	iv.SetText("")
	iv.SetCursor(0)
	iv.Render()

	if iv.HasHistorySuggestion() {
		t.Error("Expected no suggestion for empty text")
	}

	if len(iv.historySuggestions) != 0 {
		t.Errorf("Expected 0 suggestions, got %d", len(iv.historySuggestions))
	}
}

func TestInputView_HistorySuggestions_NoMatchWhenCursorNotAtEnd(t *testing.T) {
	mockModelService := createMockModelService()
	iv := createInputViewWithTheme(mockModelService)

	require.NoError(t, iv.historyManager.AddToHistory("create a pull request"))

	iv.SetText("create")
	iv.SetCursor(3)
	iv.Render()

	if iv.HasHistorySuggestion() {
		t.Error("Expected no suggestion when cursor is not at end")
	}
}

func TestInputView_HistorySuggestions_NoMatchWhenNoPrefix(t *testing.T) {
	mockModelService := createMockModelService()
	iv := createInputViewWithTheme(mockModelService)

	require.NoError(t, iv.historyManager.AddToHistory("create a pull request"))

	iv.SetText("xyz")
	iv.SetCursor(len(iv.GetInput()))
	iv.Render()

	if iv.HasHistorySuggestion() {
		t.Error("Expected no suggestion for non-matching prefix")
	}

	if len(iv.historySuggestions) != 0 {
		t.Errorf("Expected 0 suggestions, got %d", len(iv.historySuggestions))
	}
}

func TestInputView_HistorySuggestions_CaseInsensitive(t *testing.T) {
	mockModelService := createMockModelService()
	iv := createInputViewWithTheme(mockModelService)

	require.NoError(t, iv.historyManager.AddToHistory("Create a pull request"))

	iv.SetText("cre")
	iv.SetCursor(len(iv.GetInput()))
	iv.Render()

	if !iv.HasHistorySuggestion() {
		t.Error("Expected case-insensitive matching to work")
	}

	if len(iv.historySuggestions) != 1 {
		t.Errorf("Expected 1 suggestion with case-insensitive match, got %d", len(iv.historySuggestions))
	}
}

func TestInputView_HistorySuggestions_ExcludesExactMatch(t *testing.T) {
	mockModelService := createMockModelService()
	iv := createInputViewWithTheme(mockModelService)

	require.NoError(t, iv.historyManager.AddToHistory("create"))

	iv.SetText("create")
	iv.SetCursor(len(iv.GetInput()))
	iv.Render()

	if iv.HasHistorySuggestion() {
		t.Error("Expected no suggestion for exact match")
	}
}

func TestInputView_HistorySuggestions_AcceptWithNoSuggestion(t *testing.T) {
	mockModelService := createMockModelService()
	iv := createInputViewWithTheme(mockModelService)

	accepted := iv.AcceptHistorySuggestion()

	if accepted {
		t.Error("Expected AcceptHistorySuggestion to return false when no suggestion")
	}
}

func TestInputView_HistorySuggestions_TabHandling(t *testing.T) {
	mockModelService := createMockModelService()
	iv := createInputViewWithTheme(mockModelService)

	require.NoError(t, iv.historyManager.AddToHistory("create a pull request"))
	require.NoError(t, iv.historyManager.AddToHistory("create a new branch"))

	iv.SetText("create")
	iv.SetCursor(len(iv.GetInput()))
	iv.Render()

	firstSuggestion := iv.historySuggestion

	tabKey := tea.KeyPressMsg{Code: tea.KeyTab}
	_, _ = iv.HandleKey(tabKey)

	if iv.historySuggestion == firstSuggestion {
		t.Error("Expected Tab to cycle to different suggestion")
	}
}

// newInputViewWithBranch builds an InputView with the git branch cache pre-seeded
// so getCurrentGitBranch returns without shelling out to git.
func newInputViewWithBranch(t *testing.T, branch string) *InputView {
	t.Helper()
	iv := createInputViewWithTheme(createMockModelService())
	iv.gitBranchCache = branch
	iv.gitBranchCacheTime = time.Now()
	iv.gitBranchCacheTTL = 5 * time.Second
	return iv
}

func TestInputView_BuildGitBranchLabel(t *testing.T) {
	iv := newInputViewWithBranch(t, "main")
	require.Equal(t, "⎇ main", iv.buildGitBranchLabel())
}

func TestInputView_BuildGitBranchLabel_DisabledByConfig(t *testing.T) {
	iv := newInputViewWithBranch(t, "main")
	cfg := config.DefaultConfig()
	cfg.Chat.StatusBar.Indicators.GitBranch = false
	iv.config = cfg

	require.Empty(t, iv.buildGitBranchLabel())
}

func TestInputView_RenderEmbedsBranchInTopBorder(t *testing.T) {
	iv := newInputViewWithBranch(t, "feature/test-branch")
	iv.SetWidth(80)

	topLine, _, _ := strings.Cut(iv.Render(), "\n")

	require.Contains(t, topLine, "⎇")
	require.Contains(t, topLine, "feature/test-branch")
	require.Contains(t, topLine, "╮", "top border should keep its rounded right corner")
}

func TestInputView_RenderTruncatesLongBranchInBorder(t *testing.T) {
	iv := newInputViewWithBranch(t, "feature/a-really-long-branch-name-that-keeps-going-and-going")
	iv.SetWidth(80)

	topLine, _, _ := strings.Cut(iv.Render(), "\n")

	require.Contains(t, topLine, "⎇")
	require.Contains(t, topLine, "...")
	require.NotContains(t, topLine, "going-and-going")
}

func TestInputView_RenderDropsBranchWhenTooNarrow(t *testing.T) {
	iv := newInputViewWithBranch(t, "main")
	iv.SetText("hi")
	iv.SetWidth(12)

	topLine, _, _ := strings.Cut(iv.Render(), "\n")

	require.NotContains(t, topLine, "⎇")
}

func TestInputView_RenderOmitsBranchWhenDisabled(t *testing.T) {
	iv := newInputViewWithBranch(t, "main")
	cfg := config.DefaultConfig()
	cfg.Chat.StatusBar.Indicators.GitBranch = false
	iv.config = cfg
	iv.SetWidth(80)

	require.NotContains(t, iv.Render(), "⎇")
}

func TestInputView_BashCommandCompletedInvalidatesBranchCache(t *testing.T) {
	iv := newInputViewWithBranch(t, "main")
	require.NotEmpty(t, iv.gitBranchCache)

	_, _ = iv.Update(domain.BashCommandCompletedEvent{})

	require.Empty(t, iv.gitBranchCache)
}

func TestInputView_ArrowDownHandsOffToStatusBarWhenIdle(t *testing.T) {
	ta := newInputTextarea("")
	iv := &InputView{ta: ta, historyManager: history.NewMemoryOnlyHistoryManager(10)}

	require.False(t, iv.IsNavigatingHistory(), "fresh input must not be navigating history")

	_, cmd := iv.HandleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	require.NotNil(t, cmd, "expected a command handing focus to the status bar")
	_, ok := cmd().(domain.FocusStatusBarEvent)
	require.True(t, ok, "expected a FocusStatusBarEvent")
}

func TestInputView_ArrowDownNavigatesWhileInHistory(t *testing.T) {
	ta := newInputTextarea("")
	iv := &InputView{ta: ta, historyManager: history.NewMemoryOnlyHistoryManager(10)}
	require.NoError(t, iv.AddToHistory("previous message"))

	_, _ = iv.HandleKey(tea.KeyPressMsg{Code: tea.KeyUp})
	require.True(t, iv.IsNavigatingHistory(), "arrow up should enter history navigation")
	require.Equal(t, "previous message", iv.GetInput())

	_, cmd := iv.HandleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	require.Nil(t, cmd, "arrow down must keep navigating history, not hand off focus")
	require.False(t, iv.IsNavigatingHistory(), "returning to the newest entry leaves history navigation")
}

// newInputViewWithPR builds an InputView with the git branch and PR caches
// pre-seeded so getCurrentGitBranch returns without shelling out and the PR
// label renders from state.
func newInputViewWithPR(t *testing.T, branch, pr string) *InputView {
	t.Helper()
	iv := createInputViewWithTheme(createMockModelService())
	iv.gitBranchCache = branch
	iv.gitBranchCacheTime = time.Now()
	iv.gitBranchCacheTTL = 5 * time.Second
	iv.gitPRCache = pr
	return iv
}

func TestInputView_BuildGitBranchLabel_WithPR(t *testing.T) {
	iv := newInputViewWithPR(t, "fix/issue-785", "792")
	require.Equal(t, "⎇ fix/issue-785 #792", iv.buildGitBranchLabel())
}

func TestInputView_BuildGitBranchLabel_WithPR_DisabledByConfig(t *testing.T) {
	iv := newInputViewWithPR(t, "fix/issue-785", "792")
	cfg := config.DefaultConfig()
	cfg.Chat.StatusBar.Indicators.GitPR = false
	iv.config = cfg

	require.Equal(t, "⎇ fix/issue-785", iv.buildGitBranchLabel())
}

func TestInputView_BuildGitBranchLabel_WithPR_NoPR(t *testing.T) {
	iv := newInputViewWithPR(t, "main", "")
	require.Equal(t, "⎇ main", iv.buildGitBranchLabel())
}

func TestInputView_BuildGitBranchLabel_WithPR_GitBranchDisabled(t *testing.T) {
	iv := newInputViewWithPR(t, "main", "123")
	cfg := config.DefaultConfig()
	cfg.Chat.StatusBar.Indicators.GitBranch = false
	iv.config = cfg

	require.Empty(t, iv.buildGitBranchLabel())
}

func TestInputView_RenderEmbedsBranchAndPRInTopBorder(t *testing.T) {
	iv := newInputViewWithPR(t, "fix/issue-785", "792")
	iv.SetWidth(80)

	topLine, _, _ := strings.Cut(iv.Render(), "\n")

	require.Contains(t, topLine, "⎇")
	require.Contains(t, topLine, "fix/issue-785")
	require.Contains(t, topLine, "#792")
	require.Contains(t, topLine, "╮", "top border should keep its rounded right corner")
}

func TestInputView_RenderOmitsPRWhenDisabled(t *testing.T) {
	iv := newInputViewWithPR(t, "fix/issue-785", "792")
	cfg := config.DefaultConfig()
	cfg.Chat.StatusBar.Indicators.GitPR = false
	iv.config = cfg
	iv.SetWidth(80)

	topLine, _, _ := strings.Cut(iv.Render(), "\n")

	require.Contains(t, topLine, "⎇")
	require.Contains(t, topLine, "fix/issue-785")
	require.NotContains(t, topLine, "#792")
}

func TestInputView_GitPRResolvedEventStoresPR(t *testing.T) {
	iv := newInputViewWithPR(t, "main", "")

	_, cmd := iv.Update(domain.GitPRResolvedEvent{PR: "792"})

	require.Nil(t, cmd)
	require.Equal(t, "792", iv.gitPRCache)
}

func TestInputView_BashCommandCompletedRefetchesPR(t *testing.T) {
	iv := newInputViewWithPR(t, "main", "123")

	_, cmd := iv.Update(domain.BashCommandCompletedEvent{})

	require.NotNil(t, cmd, "bash completion must trigger an async PR refetch")
	require.Equal(t, "123", iv.gitPRCache, "stale value must survive until the refetch resolves (no flicker)")
}

func TestInputView_ToolExecutionCompletedRefetchesPROnBash(t *testing.T) {
	iv := newInputViewWithPR(t, "main", "123")

	_, cmd := iv.Update(domain.ToolExecutionCompletedEvent{
		Results: []*domain.ToolExecutionResult{{ToolName: "Read"}, {ToolName: "Bash"}},
	})
	require.NotNil(t, cmd, "a Bash tool result must trigger an async PR refetch")

	_, cmd = iv.Update(domain.ToolExecutionCompletedEvent{
		Results: []*domain.ToolExecutionResult{{ToolName: "Read"}},
	})
	require.Nil(t, cmd, "non-Bash tool results must not refetch")
}

func TestInputView_InitFetchesPR(t *testing.T) {
	iv := createInputViewWithTheme(createMockModelService())
	require.NotNil(t, iv.Init(), "Init must kick off the initial async PR fetch")
}

func TestInputView_BranchChangeClearsPRCache(t *testing.T) {
	iv := newInputViewWithPR(t, "definitely-not-the-real-branch", "123")
	iv.gitBranchCacheTime = time.Now().Add(-10 * time.Second)
	iv.resolveGitBranch = func() (string, error) { return "main", nil }

	_, _ = iv.getCurrentGitBranch()

	require.Empty(t, iv.gitPRCache, "a branch switch must drop the old branch's PR number")
}

func TestInputView_BranchRefreshAfterInvalidationKeepsPRCache(t *testing.T) {
	iv := newInputViewWithPR(t, "main", "123")
	iv.InvalidateGitBranchCache()
	iv.resolveGitBranch = func() (string, error) { return "main", nil }

	_, _ = iv.getCurrentGitBranch()

	require.Equal(t, "123", iv.gitPRCache, "refresh from an invalidated (empty) branch cache must not clear the PR")
}

func TestInputView_ShouldShowIndicator_GitPR(t *testing.T) {
	tests := []struct {
		name          string
		configEnabled bool
		expected      bool
	}{
		{
			name:          "git_pr enabled returns true",
			configEnabled: true,
			expected:      true,
		},
		{
			name:          "git_pr disabled returns false",
			configEnabled: false,
			expected:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Chat.StatusBar.Indicators.GitPR = tt.configEnabled

			statusBar := &InputStatusBar{
				config: cfg,
			}

			result := statusBar.shouldShowIndicator("git_pr")

			if result != tt.expected {
				t.Errorf("Expected %v but got %v", tt.expected, result)
			}
		})
	}
}
