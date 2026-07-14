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

	if iv.height != 20 {
		t.Errorf("Expected default height 20, got %d", iv.height)
	}

	if iv.modelService != mockModelService {
		t.Error("Expected model service to be set")
	}

	if iv.historyManager == nil {
		t.Error("Expected history manager to be initialized")
	}
}

func TestInputView_GettersAndSetters(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(iv *InputView)
		got    func(iv *InputView) any
		want   any
	}{
		{
			name:   "get input returns set text",
			mutate: func(iv *InputView) { iv.SetText("Hello, world!") },
			got:    func(iv *InputView) any { return iv.GetInput() },
			want:   "Hello, world!",
		},
		{
			name:   "set text stores content",
			mutate: func(iv *InputView) { iv.SetText("New text content") },
			got:    func(iv *InputView) any { return iv.GetInput() },
			want:   "New text content",
		},
		{
			name:   "set text moves cursor to end",
			mutate: func(iv *InputView) { iv.SetText("New text content") },
			got:    func(iv *InputView) any { return iv.GetCursor() },
			want:   16,
		},
		{
			name: "get cursor returns position set in text",
			mutate: func(iv *InputView) {
				iv.SetText("Hello World")
				iv.SetCursor(5)
			},
			got:  func(iv *InputView) any { return iv.GetCursor() },
			want: 5,
		},
		{
			name:   "set cursor keeps zero for invalid position",
			mutate: func(iv *InputView) { iv.SetCursor(15) },
			got:    func(iv *InputView) any { return iv.GetCursor() },
			want:   0,
		},
		{
			name: "set cursor moves within text",
			mutate: func(iv *InputView) {
				iv.SetText("Hello World")
				iv.SetCursor(5)
			},
			got:  func(iv *InputView) any { return iv.GetCursor() },
			want: 5,
		},
		{
			name:   "set width updates width",
			mutate: func(iv *InputView) { iv.SetWidth(120) },
			got:    func(iv *InputView) any { return iv.width },
			want:   120,
		},
		{
			name:   "set height updates height",
			mutate: func(iv *InputView) { iv.SetHeight(8) },
			got:    func(iv *InputView) any { return iv.height },
			want:   8,
		},
		{
			name:   "set placeholder updates placeholder",
			mutate: func(iv *InputView) { iv.SetPlaceholder("Enter your message...") },
			got:    func(iv *InputView) any { return iv.placeholder },
			want:   "Enter your message...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iv := NewInputView(createMockModelService())

			tt.mutate(iv)

			if got := tt.got(iv); got != tt.want {
				t.Errorf("Expected %v, got %v", tt.want, got)
			}
		})
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

func TestInputView_HistorySuggestions(t *testing.T) {
	tests := []struct {
		name        string
		history     []string
		input       string
		cursorAtEnd bool
		cursorPos   int
		wantHas     bool
		wantSuggest string
		wantCount   int
		accept      bool
		wantAfter   string
	}{
		{
			name:        "single match completes the entry",
			history:     []string{"create a pull request", "list files"},
			input:       "cre",
			cursorAtEnd: true,
			wantHas:     true,
			wantSuggest: "ate a pull request",
			wantCount:   1,
		},
		{
			name:        "multiple matches keep most recent first",
			history:     []string{"create a pull request", "create a new branch", "create tests"},
			input:       "create",
			cursorAtEnd: true,
			wantHas:     true,
			wantSuggest: " a pull request",
			wantCount:   3,
		},
		{
			name:        "no match when empty",
			history:     []string{"create a pull request"},
			input:       "",
			cursorAtEnd: true,
			wantHas:     false,
			wantCount:   0,
		},
		{
			name:      "no match when cursor not at end",
			history:   []string{"create a pull request"},
			input:     "create",
			cursorPos: 3,
			wantHas:   false,
			wantCount: 0,
		},
		{
			name:        "no match when no prefix",
			history:     []string{"create a pull request"},
			input:       "xyz",
			cursorAtEnd: true,
			wantHas:     false,
			wantCount:   0,
		},
		{
			name:        "case-insensitive matching",
			history:     []string{"Create a pull request"},
			input:       "cre",
			cursorAtEnd: true,
			wantHas:     true,
			wantSuggest: "ate a pull request",
			wantCount:   1,
		},
		{
			name:        "excludes exact match",
			history:     []string{"create"},
			input:       "create",
			cursorAtEnd: true,
			wantHas:     false,
			wantCount:   0,
		},
		{
			name:        "accept suggestion completes the entry",
			history:     []string{"create a pull request"},
			input:       "cre",
			cursorAtEnd: true,
			accept:      true,
			wantAfter:   "create a pull request",
		},
		{
			name:      "accept with no suggestion returns false",
			accept:    true,
			wantAfter: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iv := createInputViewWithTheme(createMockModelService())
			for _, h := range tt.history {
				require.NoError(t, iv.historyManager.AddToHistory(h))
			}
			iv.SetText(tt.input)
			if tt.cursorAtEnd {
				iv.SetCursor(len(iv.GetInput()))
			} else if tt.cursorPos != 0 {
				iv.SetCursor(tt.cursorPos)
			}
			iv.Render()

			if tt.accept {
				accepted := iv.AcceptHistorySuggestion()
				if tt.wantAfter != "" {
					require.True(t, accepted)
					require.Equal(t, tt.wantAfter, iv.GetInput())
					require.Equal(t, len(iv.GetInput()), iv.GetCursor())
					require.False(t, iv.HasHistorySuggestion())
				} else {
					require.False(t, accepted)
				}
				return
			}

			require.Equal(t, tt.wantHas, iv.HasHistorySuggestion())
			if tt.wantHas {
				require.Equal(t, tt.wantSuggest, iv.historySuggestion)
			}
			require.Len(t, iv.historySuggestions, tt.wantCount)
		})
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

	_, _ = iv.HandleKey(tabKey)
	if iv.historySuggestion != firstSuggestion {
		t.Errorf("Expected second Tab to wrap back to first suggestion %q, got %q", firstSuggestion, iv.historySuggestion)
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
	tests := []struct {
		name      string
		branch    string
		pr        string
		disablePR bool
		disableBR bool
		want      string
	}{
		{
			name:   "branch label with branch only",
			branch: "main",
			want:   "⎇ main",
		},
		{
			name:      "branch label disabled by config",
			branch:    "main",
			disableBR: true,
			want:      "",
		},
		{
			name:   "branch label with PR",
			branch: "fix/issue-785",
			pr:     "792",
			want:   "⎇ fix/issue-785  #792",
		},
		{
			name:      "branch label with PR disabled by config",
			branch:    "fix/issue-785",
			pr:        "792",
			disablePR: true,
			want:      "⎇ fix/issue-785",
		},
		{
			name:   "branch label with PR but no PR number",
			branch: "main",
			pr:     "",
			want:   "⎇ main",
		},
		{
			name:      "branch label with PR but git branch disabled",
			branch:    "main",
			pr:        "123",
			disableBR: true,
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iv := newInputViewWithPR(t, tt.branch, tt.pr)
			cfg := config.DefaultConfig()
			if tt.disableBR {
				cfg.Chat.StatusBar.Indicators.GitBranch = false
			}
			if tt.disablePR {
				cfg.Chat.StatusBar.Indicators.GitPR = false
			}
			iv.config = cfg
			require.Equal(t, tt.want, iv.buildGitBranchLabel())
		})
	}
}

func TestInputView_RenderBranchInTopBorder(t *testing.T) {
	tests := []struct {
		name     string
		branch   string
		width    int
		disabled bool
		wantIcon bool
		wantDots bool
		wantFull bool
	}{
		{
			name:     "embeds branch in top border",
			branch:   "feature/test-branch",
			width:    80,
			wantIcon: true,
			wantFull: true,
		},
		{
			name:     "truncates long branch in border",
			branch:   "feature/a-really-long-branch-name-that-keeps-going-and-going",
			width:    80,
			wantIcon: true,
			wantDots: true,
		},
		{
			name:     "drops branch when too narrow",
			branch:   "main",
			width:    12,
			wantIcon: false,
		},
		{
			name:     "omits branch when disabled",
			branch:   "main",
			width:    80,
			disabled: true,
			wantIcon: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iv := newInputViewWithBranch(t, tt.branch)
			if tt.disabled {
				cfg := config.DefaultConfig()
				cfg.Chat.StatusBar.Indicators.GitBranch = false
				iv.config = cfg
			}
			iv.SetWidth(tt.width)
			if tt.width < 80 {
				iv.SetText("hi")
			}

			rendered := iv.Render()
			if !tt.wantIcon {
				require.NotContains(t, rendered, "⎇")
				return
			}

			topLine, _, _ := strings.Cut(rendered, "\n")
			require.Contains(t, topLine, "⎇")
			if tt.wantDots {
				require.Contains(t, topLine, "...")
				require.NotContains(t, topLine, "going-and-going")
			}
			if tt.wantFull {
				require.Contains(t, topLine, tt.branch)
				require.Contains(t, topLine, "╮", "top border should keep its rounded right corner")
			}
		})
	}
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

func TestInputView_CursorMultibyteRoundTrip(t *testing.T) {
	iv := createInputViewWithTheme(createMockModelService())
	text := "héllo 🎉\nwörld"
	iv.SetText(text)

	for _, pos := range []int{0, len("héllo "), len("héllo 🎉"), len(text)} {
		iv.SetCursor(pos)
		require.Equal(t, pos, iv.GetCursor(), "round-trip byte offset %d", pos)
	}

	iv.SetCursor(len(text))
	require.Equal(t, len(text), iv.GetCursor())
}

func TestInputView_SetCursorWithSoftWrappedLines(t *testing.T) {
	iv := createInputViewWithTheme(createMockModelService())
	iv.SetWidth(20)
	long := strings.Repeat("abcdefgh ", 6)
	text := long + "\nsecond"
	iv.SetText(text)

	pos := len(long) + 1 + 3
	iv.SetCursor(pos)
	require.Equal(t, pos, iv.GetCursor())
}

func TestInputView_ApplyKeybindingsRemapsTextarea(t *testing.T) {
	iv := createInputViewWithTheme(createMockModelService())
	iv.focused = true
	_ = iv.ta.Focus()

	off := false
	cfg := &config.Config{}
	cfg.Chat.Keybindings = config.KeybindingsConfig{
		Enabled: true,
		Bindings: map[string]config.KeyBindingEntry{
			config.ActionID(config.NamespaceTextEditing, "insert_newline_ctrl"): {Keys: []string{"ctrl+n"}},
			config.ActionID(config.NamespaceTextEditing, "insert_newline_alt"):  {Enabled: &off},
		},
	}
	iv.SetConfig(cfg)

	iv.SetText("hi")
	iv.SetCursor(2)
	model, _ := iv.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	iv = model.(*InputView)
	require.Equal(t, "hi\n", iv.GetInput(), "remapped key must insert newline")

	iv.SetText("hi")
	iv.SetCursor(2)
	model, _ = iv.Update(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	iv = model.(*InputView)
	require.Equal(t, "hi", iv.GetInput(), "unbound default key must no longer insert newline")

	iv.SetText("hi")
	iv.SetCursor(2)
	model, _ = iv.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})
	iv = model.(*InputView)
	require.Equal(t, "hi", iv.GetInput(), "disabled action's key must no longer insert newline")
}

func TestInputView_UpdateEmitsAutocompleteWithSettledValues(t *testing.T) {
	iv := createInputViewWithTheme(createMockModelService())
	iv.focused = true
	_ = iv.ta.Focus()

	model, cmd := iv.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	iv = model.(*InputView)
	require.NotNil(t, cmd)

	iv.SetText("later state")

	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	require.True(t, ok, "expected BatchMsg, got %T", msg)
	found := false
	for _, sub := range batch {
		if ev, isEv := sub().(domain.AutocompleteUpdateEvent); isEv {
			require.Equal(t, "a", ev.Text)
			require.Equal(t, 1, ev.CursorPos)
			found = true
		}
	}
	require.True(t, found, "expected AutocompleteUpdateEvent in batch")
}
