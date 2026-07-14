package components

import (
	"context"
	"fmt"
	"maps"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	key "charm.land/bubbles/v2/key"
	textarea "charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	config "github.com/inference-gateway/cli/config"
	constants "github.com/inference-gateway/cli/internal/constants"
	domain "github.com/inference-gateway/cli/internal/domain"
	formatting "github.com/inference-gateway/cli/internal/formatting"
	gitdiff "github.com/inference-gateway/cli/internal/services/gitdiff"
	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
	history "github.com/inference-gateway/cli/internal/ui/history"
	inputsyntax "github.com/inference-gateway/cli/internal/ui/inputsyntax"
	keys "github.com/inference-gateway/cli/internal/ui/keys"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
)

// InputView handles user input with history, delegating text editing to
// charm.land/bubbles/v2/textarea.
type InputView struct {
	ta                   textarea.Model
	placeholder          string
	width                int
	height               int
	modelService         domain.ModelService
	imageService         domain.ImageService
	stateManager         inputViewState
	skillsService        domain.SkillsService
	shortcutRegistry     *shortcuts.Registry
	fileService          domain.FileService
	githubIssueService   domain.GitHubIssueService
	highlighter          *inputsyntax.Highlighter
	config               *config.Config
	conversationRepo     domain.ConversationRepository
	historyManager       *history.HistoryManager
	disabled             bool
	savedText            string
	savedCursor          int
	themeService         domain.ThemeService
	styleProvider        *styles.Provider
	imageAttachments     []domain.ImageAttachment
	messageQueue         domain.MessageQueue
	historySuggestion    string
	historySuggestions   []string
	historySelectedIndex int
	focused              bool
	usageHint            string
	customHint           string
	gitBranchCache       string
	gitBranchCacheTime   time.Time
	gitBranchCacheTTL    time.Duration
	gitPRCache           string
	resolveGitBranch     func() (string, error)
}

// gitCurrentBranch returns the current git branch by shelling out to git. It is
// the default resolver wired into getCurrentGitBranch; tests inject a stub via
// the resolveGitBranch field so branch resolution is deterministic without a
// real repository.
func gitCurrentBranch() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), constants.GitCommandTimeout)
	defer cancel()
	output, err := gitdiff.RunGit(ctx, "", "branch", "--show-current")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func NewInputView(modelService domain.ModelService) *InputView {
	return NewInputViewWithName(modelService, "", "")
}

func NewInputViewWithName(modelService domain.ModelService, configDir, name string) *InputView {
	if configDir == "" {
		configDir = ".infer"
	}

	var historyManager *history.HistoryManager
	if name == domain.SubagentHistoryMemoryOnly {
		historyManager = history.NewMemoryOnlyHistoryManager(5)
	} else if hm, err := history.NewHistoryManagerWithName(5, configDir, name); err != nil {
		historyManager = history.NewMemoryOnlyHistoryManager(5)
	} else {
		historyManager = hm
	}

	placeholder := "Type your message... (Press Enter to send, alt+enter or ctrl+j for newline, ? for help)"
	ta := newInputTextarea(placeholder)

	return &InputView{
		ta:                ta,
		placeholder:       placeholder,
		width:             80,
		height:            5,
		modelService:      modelService,
		historyManager:    historyManager,
		themeService:      nil,
		imageAttachments:  []domain.ImageAttachment{},
		focused:           true,
		gitBranchCacheTTL: 5 * time.Second,
		resolveGitBranch:  gitCurrentBranch,
	}
}

func newInputTextarea(placeholder string) textarea.Model {
	ta := textarea.New()
	ta.Placeholder = placeholder
	ta.CharLimit = 0
	ta.MaxHeight = 5
	ta.ShowLineNumbers = false
	ta.EndOfBufferCharacter = 0
	ta.Prompt = ""
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("enter", "ctrl+m", "ctrl+j", "alt+enter", "shift+enter", "super+enter"))
	ta.KeyMap.WordBackward = key.NewBinding(key.WithKeys("alt+left", "ctrl+left", "alt+b"))
	ta.KeyMap.WordForward = key.NewBinding(key.WithKeys("alt+right", "ctrl+right", "alt+f"))
	ta.KeyMap.DeleteCharacterBackward = key.NewBinding(key.WithKeys("backspace", "shift+backspace"))
	ta.KeyMap.DeleteCharacterForward = key.NewBinding(key.WithKeys("delete", "shift+delete"))
	ta.KeyMap.DeleteWordBackward = key.NewBinding(key.WithKeys("alt+backspace", "ctrl+w", "ctrl+backspace"))
	ta.KeyMap.DeleteWordForward = key.NewBinding(key.WithKeys("alt+delete", "ctrl+delete"))
	ta.KeyMap.LineStart = key.NewBinding(key.WithKeys("home"))
	ta.KeyMap.LineEnd = key.NewBinding(key.WithKeys("end"))
	ta.KeyMap.InputBegin = key.NewBinding(key.WithKeys("ctrl+a", "alt+<", "ctrl+home"))
	ta.KeyMap.InputEnd = key.NewBinding(key.WithKeys("ctrl+e", "alt+>", "ctrl+end"))
	return ta
}

// SetThemeService sets the theme service for this input view
func (iv *InputView) SetThemeService(themeService domain.ThemeService) {
	iv.themeService = themeService
	iv.styleProvider = styles.NewProvider(themeService)
}

// inputViewState is the narrow slice of StateManager the input view reads to
// decide whether an approval/plan overlay is active.
type inputViewState interface {
	domain.ApprovalUIManager
	domain.PlanApprovalUIManager
}

// SetStateManager sets the state manager for this input view
func (iv *InputView) SetStateManager(stateManager inputViewState) {
	iv.stateManager = stateManager
}

// SetConfig sets the config for this input view
func (iv *InputView) SetConfig(cfg *config.Config) {
	iv.config = cfg
	iv.applyKeybindings(cfg.Chat.Keybindings)
}

// applyKeybindings rebuilds the textarea KeyMap entries that correspond to
// user-configurable text_editing actions, so remaps and disables in
// keybindings.yaml take effect inside the textarea (which applies the edits
// the keybinding registry passes through to it).
func (iv *InputView) applyKeybindings(kb config.KeybindingsConfig) {
	if !kb.Enabled {
		return
	}

	effective := config.GetDefaultKeybindings()
	maps.Copy(effective, kb.Bindings)

	bind := func(binding *key.Binding, actionNames ...string) {
		var keyStrs []string
		for _, name := range actionNames {
			entry, ok := effective[config.ActionID(config.NamespaceTextEditing, name)]
			if !ok || (entry.Enabled != nil && !*entry.Enabled) {
				continue
			}
			keyStrs = append(keyStrs, entry.Keys...)
		}
		*binding = key.NewBinding(key.WithKeys(keyStrs...))
		binding.SetEnabled(len(keyStrs) > 0)
	}

	bind(&iv.ta.KeyMap.InsertNewline, "insert_newline_alt", "insert_newline_ctrl")
	bind(&iv.ta.KeyMap.CharacterBackward, "move_cursor_left")
	bind(&iv.ta.KeyMap.CharacterForward, "move_cursor_right")
	bind(&iv.ta.KeyMap.DeleteCharacterBackward, "backspace")
	bind(&iv.ta.KeyMap.DeleteBeforeCursor, "delete_to_beginning")
	bind(&iv.ta.KeyMap.DeleteWordBackward, "delete_word_backward")
	bind(&iv.ta.KeyMap.DeleteWordForward, "delete_word_forward")
	bind(&iv.ta.KeyMap.WordBackward, "move_cursor_word_left")
	bind(&iv.ta.KeyMap.WordForward, "move_cursor_word_right")
	bind(&iv.ta.KeyMap.InputBegin, "move_to_beginning")
	bind(&iv.ta.KeyMap.InputEnd, "move_to_end")
}

// SetImageService sets the image service for this input view
func (iv *InputView) SetImageService(imageService domain.ImageService) {
	iv.imageService = imageService
}

// SetConversationRepo sets the conversation repository for context usage display
func (iv *InputView) SetConversationRepo(repo domain.ConversationRepository) {
	iv.conversationRepo = repo
}

// SetSkillsService sets the skills service so "/<skill>" tokens can be
// highlighted in the input to signal they route to the agent.
func (iv *InputView) SetSkillsService(skillsService domain.SkillsService) {
	iv.skillsService = skillsService
}

// SetShortcutRegistry sets the shortcut registry so "/<shortcut>" tokens can be
// highlighted in the input.
func (iv *InputView) SetShortcutRegistry(registry *shortcuts.Registry) {
	iv.shortcutRegistry = registry
}

// SetFileService sets the file service so "@<path>" references to real files can
// be highlighted in the input.
func (iv *InputView) SetFileService(fileService domain.FileService) {
	iv.fileService = fileService
}

// SetMessageQueue sets the message queue so arrow-up can restore queued
// message content into the input field instead of navigating history.
func (iv *InputView) SetMessageQueue(mq domain.MessageQueue) {
	iv.messageQueue = mq
}

// SetGitHubIssueService enables "#<number>" highlighting in the input. The
// validator only checks the digit shape - resolution against actual repo
// issues happens at submit time in the expansion path.
func (iv *InputView) SetGitHubIssueService(s domain.GitHubIssueService) {
	iv.githubIssueService = s
}

func (iv *InputView) GetInput() string {
	return iv.ta.Value()
}

func (iv *InputView) ClearInput() {
	iv.ta.Reset()
	iv.imageAttachments = []domain.ImageAttachment{}
	iv.historyManager.ResetNavigation()
}

func (iv *InputView) SetPlaceholder(text string) {
	iv.placeholder = text
	iv.ta.Placeholder = text
}

// GetCursor returns the cursor position as a byte offset into GetInput().
// The textarea reports a rune column, so the current line's rune prefix is
// converted back to bytes before adding it to the preceding lines' lengths.
func (iv *InputView) GetCursor() int {
	text := iv.ta.Value()
	if text == "" {
		return 0
	}
	lines := strings.Split(text, "\n")
	line := iv.ta.Line()
	col := iv.ta.Column()
	pos := 0
	for i := 0; i < line && i < len(lines); i++ {
		pos += len(lines[i]) + 1
	}
	if line < len(lines) {
		runes := []rune(lines[line])
		if col > len(runes) {
			col = len(runes)
		}
		pos += len(string(runes[:col]))
	}
	if pos > len(text) {
		return len(text)
	}
	return pos
}

// SetCursor moves the cursor to the given byte offset into GetInput().
func (iv *InputView) SetCursor(position int) {
	text := iv.ta.Value()
	if position < 0 || position > len(text) || text == "" {
		return
	}
	lines := strings.Split(text, "\n")
	line := 0
	col := 0
	remaining := position
	for i, l := range lines {
		if remaining <= len(l) {
			line = i
			col = utf8.RuneCountInString(l[:remaining])
			break
		}
		remaining -= len(l) + 1
		if i == len(lines)-1 {
			line = i
			col = utf8.RuneCountInString(l)
		}
	}

	iv.ta.MoveToBegin()
	for i := 0; i < line; i++ {
		iv.ta.CursorEnd()
		iv.ta.CursorDown()
	}
	iv.ta.SetCursorColumn(col)
}

func (iv *InputView) SetText(text string) {
	iv.ta.SetValue(text)
	iv.resizeTextarea()
}

func (iv *InputView) SetWidth(width int) {
	iv.width = width
	iv.ta.SetWidth(width - 4) // account for border and "> " prefix
	iv.resizeTextarea()
}

func (iv *InputView) SetHeight(height int) {
	iv.height = height
	iv.resizeTextarea()
}

func (iv *InputView) resizeTextarea() {
	iv.ta.SetHeight(iv.textareaContentHeight())
}

func (iv *InputView) textareaContentHeight() int {
	maxHeight := max(1, iv.height-2)
	if iv.ta.MaxHeight > 0 {
		maxHeight = min(maxHeight, iv.ta.MaxHeight)
	}

	text := iv.ta.Value()
	if text == "" {
		return 1
	}

	availableWidth := max(1, iv.width-8)
	lines := 0
	for _, line := range strings.Split(text, "\n") {
		lineWidth := max(1, len([]rune(line)))
		lines += (lineWidth + availableWidth - 1) / availableWidth
	}
	return min(maxHeight, max(1, lines))
}

func (iv *InputView) Render() string {
	if !iv.disabled {
		iv.updateHistorySuggestions()
	}

	text := iv.ta.Value()
	isToolsMode := strings.HasPrefix(text, "!!")
	isBashMode := strings.HasPrefix(text, "!") && !isToolsMode

	displayText := iv.renderDisplayText()

	inputContent := fmt.Sprintf("> %s", displayText)

	focused := isBashMode || isToolsMode
	borderedInput := iv.styleProvider.RenderInputField(inputContent, iv.width-4, focused, iv.buildGitBranchLabel())

	return borderedInput
}

// buildGitBranchLabel returns the "⎇ <branch>" label embedded in the input box
// top border, or "⎇ <branch>  #<pr>" when a PR exists for the current branch
// and git_pr is enabled. Returns "" when the git_branch indicator is disabled
// or there is no branch to show (not a repo / detached HEAD). Truncation to
// fit the border is handled by the style provider, so no length cap is applied
// here.
func (iv *InputView) buildGitBranchLabel() string {
	if iv.config != nil && !iv.config.Chat.StatusBar.Indicators.GitBranch {
		return ""
	}

	branch, ok := iv.getCurrentGitBranch()
	if !ok || branch == "" {
		return ""
	}

	label := fmt.Sprintf("%s %s", icons.GitBranch, branch)

	if iv.config == nil || iv.config.Chat.StatusBar.Indicators.GitPR {
		if iv.gitPRCache != "" {
			label += "  #" + iv.gitPRCache
		}
	}

	return label
}

// getCurrentGitBranch returns the current git branch with caching.
func (iv *InputView) getCurrentGitBranch() (string, bool) {
	if time.Since(iv.gitBranchCacheTime) < iv.gitBranchCacheTTL && iv.gitBranchCache != "" {
		return iv.gitBranchCache, true
	}

	resolve := iv.resolveGitBranch
	if resolve == nil {
		resolve = gitCurrentBranch
	}
	branch, err := resolve()

	iv.gitBranchCacheTime = time.Now()

	if err != nil {
		iv.gitBranchCache = ""
		return "", false
	}

	if iv.gitBranchCache != "" && branch != iv.gitBranchCache {
		iv.gitPRCache = ""
	}
	iv.gitBranchCache = branch
	return branch, branch != ""
}

// InvalidateGitBranchCache clears the git branch cache to force a refresh.
func (iv *InputView) InvalidateGitBranchCache() {
	iv.gitBranchCache = ""
	iv.gitBranchCacheTime = time.Time{}
}

// fetchGitPRCmd resolves the PR number for the current branch off the UI
// goroutine via "gh pr view --json number --jq .number". It must never run in
// the render path: it is a network round-trip to GitHub. The error is
// deliberately swallowed because gh exits non-zero for the normal "no PR"
// case; the label simply omits the number.
// ponytail: refetch is event-driven (startup, bash commands, Bash tool runs),
// so a PR opened outside the TUI stays unknown until the next such event; add
// a slow tea.Tick refetch if that ever matters.
func fetchGitPRCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		output, err := exec.CommandContext(ctx, "gh", "pr", "view", "--json", "number", "--jq", ".number").Output()
		if err != nil {
			return domain.GitPRResolvedEvent{}
		}
		return domain.GitPRResolvedEvent{PR: strings.TrimSpace(string(output))}
	}
}

func (iv *InputView) renderDisplayText() string {
	text := iv.ta.Value()
	if text == "" {
		return iv.renderPlaceholder()
	}
	return iv.renderTextWithCursor()
}

// getDisplayTextAndCursorOffset returns the display text with mode prefixes and cursor offset adjustment
// For bash mode (!), we show "! " prefix; for tools mode (!!), we show "!! " prefix
func (iv *InputView) getDisplayTextAndCursorOffset() (displayText string, cursorOffset int) {
	text := iv.ta.Value()
	isToolsMode := strings.HasPrefix(text, "!!")
	isBashMode := strings.HasPrefix(text, "!") && !isToolsMode

	if isToolsMode {
		return "!! " + text[2:], 3
	} else if isBashMode {
		return "! " + text[1:], 2
	}
	return text, 0
}

func (iv *InputView) renderPlaceholder() string {
	if iv.disabled {
		return iv.renderDisabledPlaceholder()
	}

	if !iv.focused {
		return iv.styleProvider.RenderInputPlaceholder(iv.placeholder)
	}

	return iv.renderFocusedPlaceholder()
}

// renderDisabledPlaceholder returns placeholder text when input is disabled
func (iv *InputView) renderDisabledPlaceholder() string {
	if iv.customHint != "" {
		return iv.styleProvider.RenderDimText("⏸  " + iv.customHint)
	}

	if iv.stateManager == nil {
		return iv.styleProvider.RenderDimText("⏸  Input disabled")
	}

	if iv.stateManager.GetPlanApprovalUIState() != nil {
		return iv.styleProvider.RenderDimText("⏸  Plan approval required - use ←/→ or h/l to navigate, Enter/y to accept (auto), n to reject, s to approve each step")
	}

	if iv.stateManager.GetApprovalUIState() != nil {
		return iv.styleProvider.RenderDimText("⏸  Tool approval required - use ←/→ to navigate, Enter to confirm")
	}

	return iv.styleProvider.RenderDimText("⏸  Input disabled")
}

// renderFocusedPlaceholder returns placeholder text when input is focused
func (iv *InputView) renderFocusedPlaceholder() string {
	if len(iv.placeholder) == 0 {
		return iv.styleProvider.RenderCursor(" ")
	}

	firstChar := string(iv.placeholder[0])
	rest := ""
	if len(iv.placeholder) > 1 {
		rest = iv.placeholder[1:]
	}
	cursorChar := iv.styleProvider.RenderCursor(firstChar)
	restPlaceholder := iv.styleProvider.RenderInputPlaceholder(rest)
	return cursorChar + restPlaceholder
}

func (iv *InputView) renderTextWithCursor() string {
	text := iv.ta.Value()
	displayText, cursorOffset := iv.getDisplayTextAndCursorOffset()
	iv.resizeTextarea()

	adjustedCursor := iv.calculateAdjustedCursor(cursorOffset, len(displayText))
	before := displayText[:adjustedCursor]
	after := displayText[adjustedCursor:]
	availableWidth := iv.width - 8

	var result string
	if availableWidth > 0 {
		result = iv.renderWrappedText(before, after, availableWidth)
	} else {
		result = iv.renderUnwrappedText(before, after)
	}

	result = iv.applyModePrefixStyling(result)
	if !strings.HasPrefix(text, "!") {
		iv.ensureHighlighter()
		if iv.highlighter != nil {
			result = iv.highlighter.Highlight(result)
		}
	}
	return result
}

func (iv *InputView) calculateAdjustedCursor(cursorOffset int, displayTextLen int) int {
	adjustedCursor := iv.GetCursor()
	if cursorOffset > 0 {
		adjustedCursor = iv.calculateModeCursorOffset()
	}
	return min(adjustedCursor, displayTextLen)
}

func (iv *InputView) calculateModeCursorOffset() int {
	cursor := iv.GetCursor()
	text := iv.ta.Value()
	isToolsMode := strings.HasPrefix(text, "!!")

	if isToolsMode && cursor >= 2 {
		return cursor + 1
	}
	if !isToolsMode && cursor >= 1 {
		return cursor + 1
	}
	return cursor
}

func (iv *InputView) renderWrappedText(before, after string, availableWidth int) string {
	wrappedBefore := iv.preserveTrailingSpaces(before, availableWidth)
	wrappedAfter := formatting.WrapText(after, availableWidth)
	return iv.buildTextWithCursor(wrappedBefore, wrappedAfter)
}

func (iv *InputView) renderUnwrappedText(before, after string) string {
	return iv.buildTextWithCursor(before, after)
}

func (iv *InputView) buildTextWithCursor(before, after string) string {
	if !iv.focused {
		return iv.buildUnfocusedText(before, after)
	}

	if len(after) == 0 {
		return iv.buildEndOfTextWithCursor(before)
	}

	cursorChar := iv.styleProvider.RenderCursor(string(after[0]))
	restAfter := ""
	if len(after) > 1 {
		restAfter = after[1:]
	}
	return fmt.Sprintf("%s%s%s", before, cursorChar, restAfter)
}

func (iv *InputView) buildUnfocusedText(before, after string) string {
	if len(after) == 0 {
		cursor := iv.GetCursor()
		text := iv.ta.Value()
		if cursor == len(text) && iv.usageHint != "" {
			return before + iv.styleProvider.RenderDimText(iv.usageHint)
		}
		if cursor == len(text) && iv.historySuggestion != "" {
			return before + iv.styleProvider.RenderDimText(iv.historySuggestion)
		}
		return before
	}
	return before + after
}

func (iv *InputView) buildEndOfTextWithCursor(before string) string {
	cursor := iv.GetCursor()
	text := iv.ta.Value()
	if cursor == len(text) && iv.usageHint != "" {
		return before + iv.styleProvider.RenderCursor(" ") + iv.styleProvider.RenderDimText(iv.usageHint)
	}

	if cursor == len(text) && iv.historySuggestion != "" {
		firstGhostChar := string(iv.historySuggestion[0])
		cursorChar := iv.styleProvider.RenderCursor(firstGhostChar)
		restGhost := ""
		if len(iv.historySuggestion) > 1 {
			restGhost = iv.styleProvider.RenderDimText(iv.historySuggestion[1:])
		}
		return before + cursorChar + restGhost
	}

	return before + iv.styleProvider.RenderCursor(" ")
}

func (iv *InputView) preserveTrailingSpaces(text string, availableWidth int) string {
	wrappedText := formatting.WrapText(text, availableWidth)

	trailingSpaces := 0
	for i := len(text) - 1; i >= 0 && text[i] == ' '; i-- {
		trailingSpaces++
	}

	wrappedTrailingSpaces := 0
	for i := len(wrappedText) - 1; i >= 0 && wrappedText[i] == ' '; i-- {
		wrappedTrailingSpaces++
	}

	if trailingSpaces > wrappedTrailingSpaces {
		wrappedText += strings.Repeat(" ", trailingSpaces-wrappedTrailingSpaces)
	}
	return wrappedText
}

// ensureHighlighter lazily builds the input-token highlighter the first time it
// is needed, once the style provider is available. Each rule is included only
// when its backing service is wired, so the highlighter degrades gracefully (and
// tests that set only a theme don't crash). Validators close over iv so they
// read live service state. Rules are ordered skill-before-shortcut because they
// share the "/" sigil - the skill rule's ANSI prevents the shortcut rule from
// re-coloring a known skill (see inputsyntax.Highlight).
func (iv *InputView) ensureHighlighter() {
	if iv.highlighter != nil || iv.styleProvider == nil {
		return
	}

	var rules []inputsyntax.Rule
	if iv.skillsService != nil {
		rules = append(rules, inputsyntax.SkillRule(func(name string) bool {
			lower := strings.ToLower(name)
			if parts := strings.SplitN(lower, ":", 2); len(parts) == 2 {
				_, found := iv.skillsService.Get(parts[1])
				return found
			}
			_, found := iv.skillsService.Get(lower)
			return found
		}))
	}
	if iv.shortcutRegistry != nil {
		rules = append(rules, inputsyntax.ShortcutRule(func(name string) bool {
			_, found := iv.shortcutRegistry.Get(name)
			return found
		}))
	}
	if iv.fileService != nil {
		rules = append(rules, inputsyntax.FileRefRule(func(path string) bool {
			return iv.fileService.ValidateFile(path) == nil
		}))
	}

	if iv.githubIssueService != nil {
		rules = append(rules, inputsyntax.IssueRefRule(func(name string) bool {
			if name == "" {
				return false
			}
			for _, r := range name {
				if r < '0' || r > '9' {
					return false
				}
			}
			return true
		}))
	}

	if len(rules) == 0 {
		return
	}
	iv.highlighter = inputsyntax.New(rules, iv.styleProvider.GetThemeColor, iv.styleProvider.RenderWithColor)
}

// applyModePrefixStyling applies accent color styling to mode prefixes (! or !!)
func (iv *InputView) applyModePrefixStyling(text string) string {
	textVal := iv.ta.Value()
	isToolsMode := strings.HasPrefix(textVal, "!!")
	isBashMode := strings.HasPrefix(textVal, "!") && !isToolsMode

	if !isBashMode && !isToolsMode {
		return text
	}

	accentColor := iv.styleProvider.GetThemeColor("accent")

	if isToolsMode && strings.HasPrefix(text, "!! ") {
		styledPrefix := iv.styleProvider.RenderWithColor("!!", accentColor)
		return styledPrefix + text[2:]
	} else if isBashMode && strings.HasPrefix(text, "! ") {
		styledPrefix := iv.styleProvider.RenderWithColor("!", accentColor)
		return styledPrefix + text[1:]
	}

	return text
}

// NavigateHistoryUp moves up in history (to older messages) - public method for interface
func (iv *InputView) NavigateHistoryUp() {
	iv.navigateHistoryUp()
}

// NavigateHistoryDown moves down in history (to newer messages) - public method for interface
func (iv *InputView) NavigateHistoryDown() {
	iv.navigateHistoryDown()
}

// IsNavigatingHistory reports whether input-history navigation is active,
// i.e. whether arrow-down still has an entry to return to
func (iv *InputView) IsNavigatingHistory() bool {
	return iv.historyManager.IsNavigating()
}

// navigateHistoryUp moves up in history (to older messages).
// If the message queue is non-empty, it dequeues the queued message and
// restores its content into the input field instead of navigating history,
// so the user can edit and re-send it. The message is removed from the
// queue so the agent stops processing it.
func (iv *InputView) navigateHistoryUp() {
	if iv.messageQueue != nil && !iv.messageQueue.IsEmpty() {
		if qm := iv.messageQueue.Dequeue(); qm != nil {
			if content, err := qm.Message.Content.AsMessageContent0(); err == nil && content != "" {
				iv.SetText(content)
				iv.SetCursor(len(content))
				return
			}
		}
	}
	newText := iv.historyManager.NavigateUp(iv.ta.Value())
	iv.SetText(newText)
	iv.SetCursor(len(newText))
}

// navigateHistoryDown moves down in history (to newer messages)
func (iv *InputView) navigateHistoryDown() {
	newText := iv.historyManager.NavigateDown(iv.ta.Value())
	iv.SetText(newText)
	iv.SetCursor(len(newText))
}

// AddToHistory adds the current input to the history
func (iv *InputView) AddToHistory(text string) error {
	if text == "" {
		return nil
	}
	return iv.historyManager.AddToHistory(text)
}

// SetUsageHint sets the usage hint for ghost text display
func (iv *InputView) SetUsageHint(hint string) {
	iv.usageHint = hint
}

// GetUsageHint returns the current usage hint
func (iv *InputView) GetUsageHint() string {
	return iv.usageHint
}

// SetCustomHint sets a custom hint
// Note: The input is disabled separately by handleViewSpecificMessages based on navigation mode
func (iv *InputView) SetCustomHint(hint string) {
	iv.customHint = hint
}

// ClearCustomHint clears the custom hint
// Note: The input is re-enabled separately by handleViewSpecificMessages when exiting navigation mode
func (iv *InputView) ClearCustomHint() {
	iv.customHint = ""
}

// Bubble Tea interface
func (iv *InputView) Init() tea.Cmd { return tea.Batch(iv.ta.Focus(), fetchGitPRCmd()) }

func (iv *InputView) View() tea.View { return tea.NewView(iv.Render()) }

func (iv *InputView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	textBefore, cursorBefore := iv.ta.Value(), iv.GetCursor()
	iv.ta, cmd = iv.ta.Update(msg)

	if _, isKey := msg.(tea.KeyPressMsg); isKey && !iv.disabled {
		if text, cursor := iv.ta.Value(), iv.GetCursor(); text != textBefore || cursor != cursorBefore {
			autocompleteCmd := func() tea.Msg {
				return domain.AutocompleteUpdateEvent{Text: text, CursorPos: cursor}
			}
			return iv, tea.Batch(cmd, autocompleteCmd)
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		iv.SetWidth(msg.Width)
		return iv, cmd
	case tea.FocusMsg:
		iv.focused = true
		focusCmd := iv.ta.Focus()
		return iv, tea.Batch(cmd, focusCmd)
	case tea.BlurMsg:
		iv.focused = false
		iv.ta.Blur()
		return iv, cmd
	case domain.ClearInputEvent:
		iv.ClearInput()
		return iv, cmd
	case domain.SetInputEvent:
		iv.SetText(msg.Text)
		iv.SetCursor(len(msg.Text))
		return iv, cmd
	case domain.GitPRResolvedEvent:
		iv.gitPRCache = msg.PR
		return iv, cmd
	case domain.BashCommandCompletedEvent:
		iv.InvalidateGitBranchCache()
		return iv, tea.Batch(cmd, fetchGitPRCmd())
	case domain.ToolExecutionCompletedEvent:
		for _, result := range msg.Results {
			if result != nil && result.ToolName == "Bash" {
				return iv, tea.Batch(cmd, fetchGitPRCmd())
			}
		}
		return iv, cmd
	}
	return iv, cmd
}

func (iv *InputView) HandleKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(k, inputViewKeys.tab) {
		if iv.HasHistorySuggestion() && iv.GetCursor() == len(iv.ta.Value()) {
			iv.cycleHistorySuggestion()
			return iv, nil
		}
	}

	switch {
	case key.Matches(k, inputViewKeys.navUp):
		iv.navigateHistoryUp()
		return iv, nil
	case key.Matches(k, inputViewKeys.navDown):
		if !iv.IsNavigatingHistory() {
			return iv, func() tea.Msg { return domain.FocusStatusBarEvent{} }
		}
		iv.navigateHistoryDown()
		return iv, nil
	}

	// navUp/navDown already returned above; only cursor-movement keys should
	// preserve history navigation, everything else resets it.
	if !isNavigationKey(k) {
		iv.historyManager.ResetNavigation()
	}

	return iv, nil
}

// isNavigationKey reports whether the key is a cursor-movement key that should not
// reset history navigation.
func isNavigationKey(msg tea.KeyPressMsg) bool {
	return key.Matches(msg, inputViewKeys.navigation...)
}

func (iv *InputView) CanHandle(key tea.KeyPressMsg) bool {
	return keys.CanInputHandle(key)
}

// SetDisabled sets whether the input is disabled (prevents typing)
// When disabling, saves the current text and clears the input
// When re-enabling, restores the saved text
func (iv *InputView) SetDisabled(disabled bool) {
	if disabled && !iv.disabled {
		iv.savedText = iv.ta.Value()
		iv.savedCursor = iv.GetCursor()
		iv.ta.Reset()
	} else if !disabled && iv.disabled {
		iv.SetText(iv.savedText)
		iv.SetCursor(iv.savedCursor)
		iv.savedText = ""
		iv.savedCursor = 0
	}
	iv.disabled = disabled
}

// IsDisabled returns whether the input is disabled
func (iv *InputView) IsDisabled() bool {
	return iv.disabled
}

// AddImageAttachment adds an image attachment to the pending list
func (iv *InputView) AddImageAttachment(image domain.ImageAttachment) {
	image.DisplayName = fmt.Sprintf("Image %d", len(iv.imageAttachments)+1)
	iv.imageAttachments = append(iv.imageAttachments, image)

	imageToken := fmt.Sprintf("[%s]", image.DisplayName)
	cursor := iv.GetCursor()
	text := iv.ta.Value()
	newText := text[:cursor] + imageToken + text[cursor:]
	iv.SetText(newText)
	iv.SetCursor(cursor + len(imageToken))
}

// GetImageAttachments returns the list of pending image attachments
func (iv *InputView) GetImageAttachments() []domain.ImageAttachment {
	return iv.imageAttachments
}

// ClearImageAttachments clears all pending image attachments
func (iv *InputView) ClearImageAttachments() {
	iv.imageAttachments = []domain.ImageAttachment{}
}

// GetHistoryManager returns the history manager for external use
func (iv *InputView) GetHistoryManager() *history.HistoryManager {
	return iv.historyManager
}

// updateHistorySuggestions filters history based on current input and updates suggestions
func (iv *InputView) updateHistorySuggestions() {
	text := iv.ta.Value()
	if text == "" || iv.GetCursor() != len(text) {
		iv.historySuggestion = ""
		iv.historySuggestions = nil
		iv.historySelectedIndex = 0
		return
	}

	if iv.historyManager.IsNavigating() {
		return
	}

	count := iv.historyManager.GetHistoryCount()
	matches := make([]string, 0)

	iv.historyManager.ResetNavigation()
	tempHistory := make([]string, 0, count)

	for i := 0; i < count; i++ {
		entry := iv.historyManager.NavigateUp("")
		if entry != "" {
			tempHistory = append([]string{entry}, tempHistory...)
		}
	}
	iv.historyManager.ResetNavigation()

	query := strings.ToLower(text)
	for _, entry := range tempHistory {
		if entry != text && strings.HasPrefix(strings.ToLower(entry), query) {
			matches = append(matches, entry)
		}
	}

	iv.historySuggestions = matches
	if len(matches) > 0 {
		if iv.historySelectedIndex >= len(matches) {
			iv.historySelectedIndex = 0
		}
		iv.historySuggestion = matches[iv.historySelectedIndex][len(text):]
	} else {
		iv.historySuggestion = ""
		iv.historySelectedIndex = 0
	}
}

// cycleHistorySuggestion moves to the next suggestion in the list
func (iv *InputView) cycleHistorySuggestion() {
	if len(iv.historySuggestions) == 0 {
		return
	}

	iv.historySelectedIndex = (iv.historySelectedIndex + 1) % len(iv.historySuggestions)
	text := iv.ta.Value()
	iv.historySuggestion = iv.historySuggestions[iv.historySelectedIndex][len(text):]
}

// AcceptHistorySuggestion applies the current suggestion to the input
func (iv *InputView) AcceptHistorySuggestion() bool {
	if iv.historySuggestion == "" {
		return false
	}

	text := iv.ta.Value()
	iv.SetText(text + iv.historySuggestion)
	iv.SetCursor(len(iv.ta.Value()))
	iv.historySuggestion = ""
	iv.historySuggestions = nil
	iv.historySelectedIndex = 0
	return true
}

// TryHandleHistorySuggestionTab handles Tab key for history suggestions
// Returns true if handled (either cycled or accepted), false if no suggestion available
func (iv *InputView) TryHandleHistorySuggestionTab() bool {
	if len(iv.historySuggestions) == 0 {
		return false
	}

	if iv.historySuggestion != "" {
		iv.cycleHistorySuggestion()
		return true
	}

	return false
}

// HasHistorySuggestion returns true if there's a history suggestion available
func (iv *InputView) HasHistorySuggestion() bool {
	return iv.historySuggestion != ""
}
