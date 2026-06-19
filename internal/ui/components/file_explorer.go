package components

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	viewport "charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	chroma "github.com/alecthomas/chroma/v2"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
	ignore "github.com/sabhiram/go-gitignore"
	fuzzy "github.com/sahilm/fuzzy"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	diffview "github.com/inference-gateway/cli/internal/ui/components/diffview"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// explorerRefreshInterval is how often the explorer re-reads the expanded
// directories so created/removed/renamed files show up "live".
const explorerRefreshInterval = time.Second

const (
	explorerSidebarMinWidth = 24
	explorerSidebarMaxWidth = 44
	explorerMinPaneWidth    = 20
	explorerMaxPreviewBytes = 1 << 20 // 1 MiB; larger files are not previewed
	explorerMaxFindFiles    = 50000   // cap on the fuzzy-finder candidate walk
	explorerMaxFindResults  = 200     // cap on displayed fuzzy matches
	explorerAnnotateContext = 5       // context lines around a snippet in large files
)

// gitignoreDefaults are always-ignored entries, matching internal/agent/tools/tree.go.
var gitignoreDefaults = []string{".git/", ".DS_Store", ".infer/"}

// Configurable action IDs for the explorer panel (defaults in
// config.addExplorerBindings; users override them in keybindings.yaml).
var (
	actExpNavUp        = config.ActionID(config.NamespaceExplorer, "nav_up")
	actExpNavDown      = config.ActionID(config.NamespaceExplorer, "nav_down")
	actExpCollapse     = config.ActionID(config.NamespaceExplorer, "collapse")
	actExpExpand       = config.ActionID(config.NamespaceExplorer, "expand")
	actExpToggle       = config.ActionID(config.NamespaceExplorer, "toggle")
	actExpOpen         = config.ActionID(config.NamespaceExplorer, "open")
	actExpFind         = config.ActionID(config.NamespaceExplorer, "find")
	actExpToggleHidden = config.ActionID(config.NamespaceExplorer, "toggle_hidden")
	actExpScrollUp     = config.ActionID(config.NamespaceExplorer, "scroll_up")
	actExpScrollDown   = config.ActionID(config.NamespaceExplorer, "scroll_down")
	actExpHalfUp       = config.ActionID(config.NamespaceExplorer, "halfpage_up")
	actExpHalfDown     = config.ActionID(config.NamespaceExplorer, "halfpage_down")
	actExpCancel       = config.ActionID(config.NamespaceExplorer, "cancel")
	actExpSelect       = config.ActionID(config.NamespaceExplorer, "select")
	actExpToggleSelect = config.ActionID(config.NamespaceExplorer, "toggle_select")
	actExpAnnotate     = config.ActionID(config.NamespaceExplorer, "annotate")
	actExpSubmit       = config.ActionID(config.NamespaceExplorer, "submit")
)

// explorerNode is one filesystem entry (the cached, sorted children of a dir).
type explorerNode struct {
	relPath string // path relative to root; the stable identity key
	name    string // filepath.Base, for display
	isDir   bool
}

// explorerRow is one rendered line of the flattened tree.
type explorerRow struct {
	node     explorerNode
	depth    int
	expanded bool // dirs only: whether currently expanded
}

// explorerTickMsg drives the periodic live refresh.
type explorerTickMsg struct{}

// explorerWalkDoneMsg carries the result of the async fuzzy-finder file walk.
type explorerWalkDoneMsg struct {
	paths     []string
	truncated bool
}

// SnippetSelection is one annotated line range captured in explorer select
// mode. Line numbers are 1-indexed and inclusive. The Annotation is the
// natural-language instruction the user attached to the highlighted range.
type SnippetSelection struct {
	File       string
	StartLine  int
	EndLine    int
	Annotation string
}

// FileExplorerImpl is the VS Code-style file explorer side panel: a left tree of
// the working directory (lazy, collapsible, .gitignore-aware) plus a scrollable,
// syntax-highlighted preview of the selected file. A `/` fuzzy finder jumps to any
// file; `v` opens the selection in the user's real editor. It owns its full
// [sidebar | divider | pane] region; the chat input is composed beneath the pane.
type FileExplorerImpl struct {
	root          string
	styleProvider *styles.Provider
	themeService  domain.ThemeService
	keymap        diffKeymap

	width        int
	height       int
	sidebarWidth int
	paneWidth    int

	// Lazy tree model, keyed by path relative to root ("" = root).
	children map[string][]explorerNode // dir relPath -> immediate children (cached on expand)
	expanded map[string]bool           // dir relPath -> expanded? (root is always expanded)
	rows     []explorerRow             // flattened visible rows

	cursor        int
	selectedKey   string // relPath of the selected row, for re-anchor across refresh
	sidebarScroll int

	showHidden bool          // include dotfiles + gitignored entries
	ignore     *ignoreFilter // current gitignore matcher (rebuilt on showHidden toggle)

	// Preview pane.
	viewport     viewport.Model
	previewKey   string
	previewWidth int
	dirtyPreview bool
	previewRaw   string // cached highlighted content before gutter markers

	// Fuzzy finder (find mode).
	findMode      bool
	findQuery     string
	candidates    []string
	filtered      []string
	findCursor    int
	walking       bool
	walkTruncated bool

	// Edit mode - the user's real editor runs in a PTY rendered into the pane.
	editMode bool
	editor   *ptyEditor

	// Select mode - line-range selection + annotation within the preview pane.
	// The preview cursor (previewCursor) is a 0-indexed line within the current
	// file; selAnchor (-1 = none) marks the other end of an inclusive range.
	// Captured ranges accumulate in selections and are injected into the chat
	// on submit (done = true).
	selectMode    bool
	previewCursor int
	previewLines  int
	selAnchor     int
	selections    []SnippetSelection
	annotateMode  bool
	annotateInput string

	loadErr error
	done    bool
	cancel  bool
}

// NewFileExplorer creates an explorer rooted at the given working directory.
func NewFileExplorer(root string, styleProvider *styles.Provider, themeService domain.ThemeService, kb config.KeybindingsConfig) *FileExplorerImpl {
	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	vp.SetContent("")
	vp.MouseWheelEnabled = true
	vp.MouseWheelDelta = 3

	t := &FileExplorerImpl{
		root:          root,
		styleProvider: styleProvider,
		themeService:  themeService,
		keymap:        diffKeymap{keys: config.ResolveNamespaceBindings(kb, config.NamespaceExplorer)},
		children:      make(map[string][]explorerNode),
		expanded:      make(map[string]bool),
		viewport:      vp,
		dirtyPreview:  true,
		ignore:        newIgnoreFilter(root, false),
		selAnchor:     -1,
	}
	t.ensureChildren("")
	t.flatten()
	t.reanchorSelection()
	return t
}

func (t *FileExplorerImpl) Init() tea.Cmd { return t.tickCmd() }

// Reset clears state so the panel can be reused on a later open.
func (t *FileExplorerImpl) Reset() {
	t.done = false
	t.cancel = false
	t.loadErr = nil
	t.children = make(map[string][]explorerNode)
	t.expanded = make(map[string]bool)
	t.rows = nil
	t.cursor = 0
	t.selectedKey = ""
	t.sidebarScroll = 0
	t.showHidden = false
	t.ignore = newIgnoreFilter(t.root, false)
	t.previewKey = ""
	t.dirtyPreview = true
	t.findMode = false
	t.findQuery = ""
	t.candidates = nil
	t.filtered = nil
	t.findCursor = 0
	t.walking = false
	t.walkTruncated = false
	t.selectMode = false
	t.previewCursor = 0
	t.previewLines = 0
	t.selAnchor = -1
	t.selections = nil
	t.annotateMode = false
	t.annotateInput = ""
	if t.editor != nil {
		t.editor.close()
	}
	t.editMode = false
	t.editor = nil
	t.ensureChildren("")
	t.flatten()
	t.reanchorSelection()
}

func (t *FileExplorerImpl) IsDone() bool      { return t.done }
func (t *FileExplorerImpl) IsCancelled() bool { return t.cancel }

// PaneWidth returns the current preview-pane width so the caller can size the
// input row that sits beneath the pane.
func (t *FileExplorerImpl) PaneWidth() int { return t.paneWidth }

func (t *FileExplorerImpl) SetWidth(w int) {
	t.width = w
	sidebar := clampInt(w*30/100, explorerSidebarMinWidth, explorerSidebarMaxWidth)
	if sidebar > w-explorerMinPaneWidth {
		sidebar = max(w-explorerMinPaneWidth, 1)
	}
	t.sidebarWidth = sidebar
	t.paneWidth = max(w-sidebar-1, 1)
}

func (t *FileExplorerImpl) SetHeight(h int) { t.height = h }

// HintText returns the footer hint for the current mode.
func (t *FileExplorerImpl) HintText() string {
	if t.editMode && t.editor != nil {
		return "(editor) - :wq to save & return"
	}
	if t.annotateMode {
		return "(annotate) type instruction · enter confirm · esc cancel"
	}
	if t.selectMode {
		return fmt.Sprintf("(select) %s/%s move · %s range · %s annotate · %s submit · esc back",
			t.keymap.display(actExpNavUp), t.keymap.display(actExpNavDown),
			t.keymap.display(actExpToggleSelect), t.keymap.display(actExpAnnotate),
			t.keymap.display(actExpSubmit))
	}
	if t.findMode {
		return "type to filter · ↑/↓ select · enter open · esc back to tree"
	}
	return fmt.Sprintf("%s/%s select · %s/%s expand · %s find · %s open · %s select · %s hidden · %s back",
		t.keymap.display(actExpNavUp), t.keymap.display(actExpNavDown),
		t.keymap.display(actExpExpand), t.keymap.display(actExpCollapse),
		t.keymap.display(actExpFind), t.keymap.display(actExpOpen),
		t.keymap.display(actExpSelect),
		t.keymap.display(actExpToggleHidden), t.keymap.display(actExpCancel))
}

// --- update ---

func (t *FileExplorerImpl) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if t.editMode {
		return t.updateEditor(msg)
	}
	switch m := msg.(type) {
	case explorerTickMsg:
		t.handleTick()
		return t, t.tickCmd()
	case explorerWalkDoneMsg:
		t.handleWalkDone(m)
		return t, nil
	case tea.WindowSizeMsg:
		t.SetWidth(m.Width)
		t.SetHeight(m.Height)
		t.dirtyPreview = true
		return t, nil
	case tea.MouseWheelMsg:
		t.handleWheel(m)
		return t, nil
	case tea.KeyMsg:
		return t.handleKey(m)
	}
	return t, nil
}

func (t *FileExplorerImpl) handleWheel(msg tea.MouseWheelMsg) {
	switch msg.Button {
	case tea.MouseWheelUp:
		t.viewport.ScrollUp(t.viewport.MouseWheelDelta)
	case tea.MouseWheelDown:
		t.viewport.ScrollDown(t.viewport.MouseWheelDelta)
	}
}

func (t *FileExplorerImpl) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if t.annotateMode {
		return t.handleAnnotateKey(msg)
	}
	if t.selectMode {
		return t.handleSelectKey(msg)
	}
	if t.findMode {
		return t.handleFindKey(msg)
	}
	pressed := normalizeKey(msg.String())
	if pressed == "ctrl+c" { // universal escape; intentionally not remappable
		t.cancel = true
		return t, nil
	}
	switch t.keymap.match(pressed,
		actExpNavUp, actExpNavDown, actExpToggle, actExpExpand, actExpCollapse,
		actExpOpen, actExpFind, actExpToggleHidden, actExpSelect,
		actExpScrollUp, actExpScrollDown, actExpHalfUp, actExpHalfDown, actExpCancel) {
	case actExpCancel:
		t.cancel = true
	case actExpNavUp:
		t.moveCursor(-1)
	case actExpNavDown:
		t.moveCursor(1)
	case actExpToggle:
		t.toggleSelected()
	case actExpExpand:
		t.setExpanded(true)
	case actExpCollapse:
		t.setExpanded(false)
	case actExpOpen:
		return t, t.enterEditCmd()
	case actExpFind:
		return t, t.enterFind()
	case actExpSelect:
		t.enterSelectMode()
	case actExpToggleHidden:
		t.toggleHidden()
	case actExpScrollUp:
		t.viewport.ScrollUp(10)
	case actExpScrollDown:
		t.viewport.ScrollDown(10)
	case actExpHalfUp:
		t.viewport.ScrollUp(5)
	case actExpHalfDown:
		t.viewport.ScrollDown(5)
	}
	return t, nil
}

func (t *FileExplorerImpl) moveCursor(delta int) {
	if len(t.rows) == 0 {
		return
	}
	prev := t.selectedFilePath()
	t.cursor = clampInt(t.cursor+delta, 0, len(t.rows)-1)
	t.selectedKey = t.rows[t.cursor].node.relPath
	if t.selectedFilePath() != prev {
		t.dirtyPreview = true
		// Leaving the current file resets select-mode state (the preview cursor
		// and anchor belong to the previous file); captured selections persist.
		if t.selectMode {
			t.exitSelectMode()
		}
	}
}

// toggleSelected expands/collapses a folder; on a file it is a no-op (the
// preview already follows the selection).
func (t *FileExplorerImpl) toggleSelected() {
	row, ok := t.currentRow()
	if !ok || !row.node.isDir {
		return
	}
	t.setExpandedKey(row.node.relPath, !t.expanded[row.node.relPath])
}

func (t *FileExplorerImpl) setExpanded(expanded bool) {
	row, ok := t.currentRow()
	if !ok || !row.node.isDir {
		return
	}
	t.setExpandedKey(row.node.relPath, expanded)
}

func (t *FileExplorerImpl) setExpandedKey(rel string, expanded bool) {
	if expanded {
		t.expanded[rel] = true
		t.ensureChildren(rel)
	} else {
		delete(t.expanded, rel)
	}
	t.flatten()
	t.reanchorSelection()
}

// toggleHidden flips inclusion of dotfiles/gitignored entries and reloads.
func (t *FileExplorerImpl) toggleHidden() {
	t.showHidden = !t.showHidden
	t.ignore = newIgnoreFilter(t.root, t.showHidden)
	t.children = make(map[string][]explorerNode)
	t.candidates = nil // force a fresh fuzzy walk under the new filter
	t.ensureChildren("")
	t.flatten()
	t.reanchorSelection()
}

func (t *FileExplorerImpl) handleTick() {
	// Drop the cache and re-flatten: flatten() re-reads only the visible
	// (expanded) directories via ensureChildren, so new/removed files there show
	// up; collapsed directories are not re-read until expanded.
	t.children = make(map[string][]explorerNode)
	t.ensureChildren("")
	t.flatten()
	t.reanchorSelection()
	t.dirtyPreview = true
}

func (t *FileExplorerImpl) tickCmd() tea.Cmd {
	return tea.Tick(explorerRefreshInterval, func(_ time.Time) tea.Msg {
		return explorerTickMsg{}
	})
}

// --- edit mode (real editor in a PTY) ---

// enterEditCmd launches the user's editor ($VISUAL/$EDITOR/vim) on the selected
// file in a PTY rendered into the pane. The returned cmd streams the editor's
// terminal output back as ptyOutputMsg/ptyExitMsg.
func (t *FileExplorerImpl) enterEditCmd() tea.Cmd {
	rel := t.selectedFilePath()
	if rel == "" {
		return nil
	}
	abs := filepath.Join(t.root, rel)
	editor, readCmd, err := startPTYEditor(abs, t.root, t.paneWidth, max(t.height-1, 1), themeIsDark(t.styleProvider))
	if err != nil {
		t.loadErr = fmt.Errorf("failed to open editor: %w", err)
		return nil
	}
	t.editor = editor
	t.editMode = true
	return readCmd
}

// updateEditor drives the embedded editor: it forwards keys to the PTY, feeds
// PTY output into the emulator (re-arming the reader), and on child exit closes
// the editor and refreshes the tree so any change shows immediately.
func (t *FileExplorerImpl) updateEditor(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case ptyOutputMsg:
		t.editor.term.write(m.data)
		return t, t.editor.readCmd()
	case ptyExitMsg:
		t.editor.close()
		t.editor = nil
		t.editMode = false
		t.dirtyPreview = true
		t.handleTick()
		return t, nil
	case tea.KeyPressMsg:
		t.editor.write(encodeKey(m))
		return t, nil
	case explorerTickMsg:
		// Keep the live-refresh tick chain alive while editing.
		return t, t.tickCmd()
	case tea.WindowSizeMsg:
		t.SetWidth(m.Width)
		t.SetHeight(m.Height)
	}
	return t, nil
}

// --- fuzzy finder ---

func (t *FileExplorerImpl) enterFind() tea.Cmd {
	t.findMode = true
	t.findQuery = ""
	t.findCursor = 0
	if t.candidates == nil && !t.walking {
		t.walking = true
		return t.startWalkCmd()
	}
	t.applyFilter()
	return nil
}

func (t *FileExplorerImpl) exitFind() {
	t.findMode = false
	t.findQuery = ""
	t.findCursor = 0
}

func (t *FileExplorerImpl) handleFindKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch normalizeKey(msg.String()) {
	case "ctrl+c":
		t.cancel = true
		return t, nil
	case "esc":
		t.exitFind()
		return t, nil
	case "up":
		t.findCursor = clampInt(t.findCursor-1, 0, max(len(t.filtered)-1, 0))
		return t, nil
	case "down":
		t.findCursor = clampInt(t.findCursor+1, 0, max(len(t.filtered)-1, 0))
		return t, nil
	case "enter":
		t.acceptFind()
		return t, nil
	case "backspace":
		if r := []rune(t.findQuery); len(r) > 0 {
			t.findQuery = string(r[:len(r)-1])
			t.applyFilter()
		}
		return t, nil
	}
	if kp, ok := msg.(tea.KeyPressMsg); ok && kp.Text != "" {
		t.findQuery += kp.Text
		t.applyFilter()
	}
	return t, nil
}

func (t *FileExplorerImpl) applyFilter() {
	t.findCursor = 0
	t.filtered = t.filtered[:0]
	if t.findQuery == "" {
		for i, p := range t.candidates {
			if i >= explorerMaxFindResults {
				break
			}
			t.filtered = append(t.filtered, p)
		}
		return
	}
	for i, m := range fuzzy.Find(t.findQuery, t.candidates) {
		if i >= explorerMaxFindResults {
			break
		}
		t.filtered = append(t.filtered, m.Str)
	}
}

// acceptFind reveals the highlighted match in the tree (expanding its ancestor
// folders), selects it, and exits find mode.
func (t *FileExplorerImpl) acceptFind() {
	if t.findCursor < 0 || t.findCursor >= len(t.filtered) {
		t.exitFind()
		return
	}
	t.revealPath(t.filtered[t.findCursor])
	t.exitFind()
}

func (t *FileExplorerImpl) revealPath(rel string) {
	parts := strings.Split(rel, "/")
	cur := ""
	for i := 0; i < len(parts)-1; i++ {
		if cur == "" {
			cur = parts[i]
		} else {
			cur += "/" + parts[i]
		}
		t.expanded[cur] = true
	}
	t.flatten()
	if i, ok := t.indexOfRel(rel); ok {
		t.cursor = i
		t.selectedKey = rel
		t.dirtyPreview = true
	}
}

func (t *FileExplorerImpl) handleWalkDone(msg explorerWalkDoneMsg) {
	t.walking = false
	t.candidates = msg.paths
	t.walkTruncated = msg.truncated
	if t.findMode {
		t.applyFilter()
	}
}

func (t *FileExplorerImpl) startWalkCmd() tea.Cmd {
	root := t.root
	showHidden := t.showHidden
	return func() tea.Msg {
		paths, truncated := walkProject(root, showHidden)
		return explorerWalkDoneMsg{paths: paths, truncated: truncated}
	}
}

// --- select mode (line-range selection + annotation) ---

// enterSelectMode activates line-range selection on the currently previewed file.
// It is a no-op when no file is selected or the preview has no lines (binary /
// oversized placeholder). The preview cursor starts at the viewport's current
// scroll position so the user sees where they are.
func (t *FileExplorerImpl) enterSelectMode() {
	if t.selectedFilePath() == "" || t.previewLines <= 0 {
		return
	}
	t.selectMode = true
	t.selAnchor = -1
	t.previewCursor = clampInt(t.viewport.YOffset(), 0, t.previewLines-1)
}

// exitSelectMode returns to tree navigation, clearing the active range anchor.
// Captured selections are preserved so they can still be submitted.
func (t *FileExplorerImpl) exitSelectMode() {
	t.selectMode = false
	t.selAnchor = -1
}

// previewSelectionRange returns the inclusive 0-indexed [lo,hi] line range of
// the active selection, or ok=false when no anchor is set. Mirrors the diff
// viewer's patchSelectionRange.
func (t *FileExplorerImpl) previewSelectionRange() (lo, hi int, ok bool) {
	if t.selAnchor < 0 {
		return 0, 0, false
	}
	lo, hi = t.selAnchor, t.previewCursor
	if lo > hi {
		lo, hi = hi, lo
	}
	max := t.previewLines - 1
	if hi > max {
		hi = max
	}
	if lo > max {
		lo = max
	}
	return lo, hi, true
}

// movePreviewCursor moves the line cursor within the preview, clamping to the
// file's line count and scrolling the viewport to keep the cursor visible.
func (t *FileExplorerImpl) movePreviewCursor(delta int) {
	if t.previewLines <= 0 {
		return
	}
	t.previewCursor = clampInt(t.previewCursor+delta, 0, t.previewLines-1)
	t.viewport.SetYOffset(t.previewCursor)
}

func (t *FileExplorerImpl) handleSelectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	pressed := normalizeKey(msg.String())
	if pressed == "ctrl+c" { // universal escape; intentionally not remappable
		t.cancel = true
		return t, nil
	}
	switch t.keymap.match(pressed,
		actExpNavUp, actExpNavDown, actExpToggleSelect, actExpAnnotate,
		actExpSubmit, actExpCancel) {
	case actExpCancel:
		// esc exits select mode (preserving selections); q (or any other cancel
		// binding) closes the explorer entirely.
		if pressed == "esc" {
			t.exitSelectMode()
		} else {
			t.cancel = true
		}
	case actExpNavUp:
		t.movePreviewCursor(-1)
	case actExpNavDown:
		t.movePreviewCursor(1)
	case actExpToggleSelect:
		if t.selAnchor >= 0 {
			t.selAnchor = -1
		} else {
			t.selAnchor = t.previewCursor
		}
	case actExpAnnotate:
		// No explicit range: treat the single cursor line as a 1-line selection.
		if t.selAnchor < 0 {
			t.selAnchor = t.previewCursor
		}
		t.annotateMode = true
		t.annotateInput = ""
	case actExpSubmit:
		if len(t.selections) > 0 {
			t.done = true
		}
	}
	return t, nil
}

// handleAnnotateKey drives the inline annotation text input. enter confirms
// (storing the selection), esc cancels (keeping the anchor so the user can
// retry). Mirrors handleFindKey's typing model.
func (t *FileExplorerImpl) handleAnnotateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch normalizeKey(msg.String()) {
	case "ctrl+c":
		t.cancel = true
		return t, nil
	case "esc":
		t.annotateMode = false
		t.annotateInput = ""
		return t, nil
	case "enter":
		t.confirmAnnotation()
		return t, nil
	case "backspace":
		if r := []rune(t.annotateInput); len(r) > 0 {
			t.annotateInput = string(r[:len(r)-1])
		}
		return t, nil
	}
	if kp, ok := msg.(tea.KeyPressMsg); ok && kp.Text != "" {
		t.annotateInput += kp.Text
	}
	return t, nil
}

// confirmAnnotation stores the current range + typed instruction as a
// SnippetSelection and exits annotate mode. Line numbers are converted from
// 0-indexed to 1-indexed inclusive.
func (t *FileExplorerImpl) confirmAnnotation() {
	lo, hi, ok := t.previewSelectionRange()
	if !ok {
		lo = t.previewCursor
		hi = t.previewCursor
	}
	t.selections = append(t.selections, SnippetSelection{
		File:       t.selectedFilePath(),
		StartLine:  lo + 1,
		EndLine:    hi + 1,
		Annotation: t.annotateInput,
	})
	t.annotateMode = false
	t.annotateInput = ""
	t.selAnchor = -1
}

// Selections returns the annotated line ranges captured during select mode.
// The app reads this on submit (IsDone) to build the LLM context.
func (t *FileExplorerImpl) Selections() []SnippetSelection {
	return t.selections
}

// FormatAnnotations builds a structured, LLM-ready prompt from a set of
// annotated snippet selections. Selections are grouped by file (deduping
// file reads). When a file fits under explorerMaxPreviewBytes the full file
// is included once in a fenced block; larger files instead include each
// snippet with a small context window (±explorerAnnotateContext lines).
//
// The output format is:
//
//	# Code annotations
//
//	## <file>
//	```<ext>
//	<full file or windowed snippet>
//	```
//
//	### Lines <start>-<end>: <annotation>
//
//	(and repeats per selection)
//
// This is a pure function (no receiver state) so it is independently
// unit-testable.
func FormatAnnotations(root string, sels []SnippetSelection) string {
	if len(sels) == 0 {
		return ""
	}

	// Group selections by file, preserving first-seen order.
	fileOrder := make([]string, 0, len(sels))
	byFile := make(map[string][]SnippetSelection)
	for _, s := range sels {
		if _, ok := byFile[s.File]; !ok {
			fileOrder = append(fileOrder, s.File)
		}
		byFile[s.File] = append(byFile[s.File], s)
	}

	var b strings.Builder
	b.WriteString("# Code annotations\n\nThe following snippets were selected in the file explorer and annotated with instructions. Apply each instruction to the highlighted lines.\n")

	for _, file := range fileOrder {
		sels := byFile[file]
		fmt.Fprintf(&b, "\n## %s\n", file)

		abs := filepath.Join(root, file)
		data, err := os.ReadFile(abs)
		if err != nil {
			fmt.Fprintf(&b, "(file unavailable: %s)\n", err)
			for _, s := range sels {
				b.WriteString(formatAnnotationMeta(s))
			}
			continue
		}

		allLines := strings.Split(string(data), "\n")
		ext := snippetExt(file)

		if len(data) <= explorerMaxPreviewBytes {
			// Include the full file once, then list annotations as references.
			b.WriteString("```" + ext + "\n")
			for i, line := range allLines {
				fmt.Fprintf(&b, "%-*d │ %s\n", lineNumWidth(len(allLines)), i+1, line)
			}
			b.WriteString("```\n\n")
			for _, s := range sels {
				b.WriteString(formatAnnotationMeta(s))
			}
		} else {
			// Large file: include each snippet with a context window.
			for _, s := range sels {
				lo := clampInt(s.StartLine-1-explorerAnnotateContext, 0, len(allLines)-1)
				hi := clampInt(s.EndLine-1+explorerAnnotateContext, 0, len(allLines)-1)
				fmt.Fprintf(&b, "### Lines %d-%d (context %d-%d)\n", s.StartLine, s.EndLine, lo+1, hi+1)
				b.WriteString("```" + ext + "\n")
				for i := lo; i <= hi; i++ {
					marker := "  "
					if i >= s.StartLine-1 && i <= s.EndLine-1 {
						marker = "▶ "
					}
					fmt.Fprintf(&b, "%s%-*d │ %s\n", marker, lineNumWidth(hi+1), i+1, allLines[i])
				}
				b.WriteString("```\n")
				b.WriteString(formatAnnotationMeta(s))
			}
		}
	}
	return b.String()
}

// formatAnnotationMeta emits the instruction line for a selection.
func formatAnnotationMeta(s SnippetSelection) string {
	if s.Annotation == "" {
		return fmt.Sprintf("- Lines %d-%d (no annotation)\n", s.StartLine, s.EndLine)
	}
	return fmt.Sprintf("- Lines %d-%d: %s\n", s.StartLine, s.EndLine, s.Annotation)
}

// snippetExt returns the fenced-code extension for a file path.
func snippetExt(file string) string {
	ext := strings.TrimPrefix(filepath.Ext(file), ".")
	if ext == "" {
		return ""
	}
	return ext
}

// lineNumWidth returns the number of digits needed to render n (for aligning
// line-number gutters).
func lineNumWidth(n int) int {
	if n <= 0 {
		return 1
	}
	w := 0
	for n > 0 {
		n /= 10
		w++
	}
	return w
}

// --- tree model ---

// ensureChildren reads and caches a directory's immediate children (filtered and
// sorted) the first time it is needed. Read errors cache an empty list so we
// don't retry every frame.
func (t *FileExplorerImpl) ensureChildren(rel string) {
	if _, ok := t.children[rel]; ok {
		return
	}
	dirAbs := filepath.Join(t.root, rel)
	entries, err := os.ReadDir(dirAbs)
	if err != nil {
		t.children[rel] = []explorerNode{}
		return
	}
	nodes := make([]explorerNode, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		childRel := name
		if rel != "" {
			childRel = rel + "/" + name
		}
		if t.ignore.shouldHide(childRel, name, dirAbs, e.IsDir()) {
			continue
		}
		nodes = append(nodes, explorerNode{relPath: childRel, name: name, isDir: e.IsDir()})
	}
	sortNodes(nodes)
	t.children[rel] = nodes
}

// sortNodes orders directories before files, then case-insensitively by name.
func sortNodes(nodes []explorerNode) {
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].isDir != nodes[j].isDir {
			return nodes[i].isDir
		}
		return strings.ToLower(nodes[i].name) < strings.ToLower(nodes[j].name)
	})
}

func (t *FileExplorerImpl) flatten() {
	t.rows = t.rows[:0]
	t.flattenInto("", 0)
}

func (t *FileExplorerImpl) flattenInto(rel string, depth int) {
	for _, n := range t.children[rel] {
		expanded := n.isDir && t.expanded[n.relPath]
		t.rows = append(t.rows, explorerRow{node: n, depth: depth, expanded: expanded})
		if expanded {
			t.ensureChildren(n.relPath)
			t.flattenInto(n.relPath, depth+1)
		}
	}
}

func (t *FileExplorerImpl) reanchorSelection() {
	if len(t.rows) == 0 {
		t.cursor = 0
		t.selectedKey = ""
		return
	}
	if i, ok := t.indexOfRel(t.selectedKey); ok {
		t.cursor = i
	} else {
		t.cursor = clampInt(t.cursor, 0, len(t.rows)-1)
	}
	t.selectedKey = t.rows[t.cursor].node.relPath
}

func (t *FileExplorerImpl) indexOfRel(rel string) (int, bool) {
	if rel == "" {
		return 0, false
	}
	for i, r := range t.rows {
		if r.node.relPath == rel {
			return i, true
		}
	}
	return 0, false
}

func (t *FileExplorerImpl) currentRow() (explorerRow, bool) {
	if t.cursor < 0 || t.cursor >= len(t.rows) {
		return explorerRow{}, false
	}
	return t.rows[t.cursor], true
}

// selectedFilePath returns the relative path of the selected row if it is a
// file, else "".
func (t *FileExplorerImpl) selectedFilePath() string {
	if r, ok := t.currentRow(); ok && !r.node.isDir {
		return r.node.relPath
	}
	return ""
}

// --- preview ---

// chromaStyle picks a chroma highlighting style by the active theme's brightness.
func (t *FileExplorerImpl) chromaStyle() *chroma.Style {
	if theme := t.styleProvider.GetCurrentTheme(); theme != nil && isLightTheme(theme) {
		return chromastyles.Get("github")
	}
	return chromastyles.Get("github-dark")
}

// ensurePreview (re)renders the selected file into the viewport, gated so the
// read + highlight only run when the selection or width actually changed. The
// raw highlighted content is cached in previewRaw; selection gutters are
// applied separately by applyPreviewGutters so cursor/anchor moves refresh
// without re-tokenising the file.
func (t *FileExplorerImpl) ensurePreview(rel string, width int) {
	if !t.dirtyPreview && rel == t.previewKey && width == t.previewWidth {
		t.applyPreviewGutters()
		return
	}
	raw, lines := t.computePreviewRaw(rel)
	changed := rel != t.previewKey
	t.previewKey = rel
	t.previewWidth = width
	t.dirtyPreview = false
	t.previewRaw = raw
	t.previewLines = lines
	if changed {
		t.viewport.GotoTop()
		if t.previewCursor >= t.previewLines {
			t.previewCursor = max(t.previewLines-1, 0)
		}
	}
	t.applyPreviewGutters()
}

// applyPreviewGutters sets the viewport content from previewRaw, prefixing the
// cursor line (▶) and selected range lines (▌) when select mode is active. The
// marker is a 2-char gutter so it stays alignment-safe with the line-number
// prefix produced by diffview.Highlight. Cheap enough to run every render.
func (t *FileExplorerImpl) applyPreviewGutters() {
	raw := t.previewRaw
	if !t.selectMode && len(t.selections) == 0 {
		t.viewport.SetContent(raw)
		return
	}
	lines := strings.Split(raw, "\n")
	accent := t.styleProvider.GetThemeColor("accent")
	cursorGutter := t.styleProvider.RenderWithColorAndBold("▶ ", accent)
	selGutter := t.styleProvider.RenderWithColor("▌ ", accent)
	lo, hi, hasSel := t.previewSelectionRange()
	for i := range lines {
		marker := "  "
		switch {
		case t.selectMode && i == t.previewCursor:
			marker = cursorGutter
		case hasSel && i >= lo && i <= hi:
			marker = selGutter
		}
		lines[i] = marker + lines[i]
	}
	t.viewport.SetContent(strings.Join(lines, "\n"))
}

func (t *FileExplorerImpl) computePreview(rel string) string {
	raw, _ := t.computePreviewRaw(rel)
	return raw
}

// computePreviewRaw returns the highlighted content and the number of source
// lines (0 for binary/oversized/errored placeholders, which can't be selected).
func (t *FileExplorerImpl) computePreviewRaw(rel string) (string, int) {
	abs := filepath.Join(t.root, rel)
	info, err := os.Stat(abs)
	if err != nil {
		return t.styleProvider.RenderErrorText("Failed to read file: " + err.Error()), 0
	}
	if info.Size() > explorerMaxPreviewBytes {
		return t.styleProvider.RenderDimText("⊘ Binary or large file - not shown"), 0
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return t.styleProvider.RenderErrorText("Failed to read file: " + err.Error()), 0
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return t.styleProvider.RenderDimText("⊘ Binary or large file - not shown"), 0
	}
	src := string(data)
	highlighted := diffview.Highlight(rel, src, t.chromaStyle(), true)
	lines := strings.Count(src, "\n")
	if len(src) > 0 && !strings.HasSuffix(src, "\n") {
		lines++
	}
	return highlighted, lines
}

// --- rendering ---

// View satisfies tea.Model. The app composes the real layout via Render.
func (t *FileExplorerImpl) View() tea.View {
	return tea.NewView(t.Render(""))
}

// Render lays out the full region: a full-height sidebar and divider on the
// left, and on the right the preview pane with the (already-rendered) input row
// stacked beneath it. Pass "" for inputRow to render the pane at full height.
func (t *FileExplorerImpl) Render(inputRow string) string {
	if t.width <= 0 || t.height <= 0 {
		return ""
	}
	sidebar := t.renderSidebar(t.sidebarWidth, t.height)
	divider := t.renderDivider(t.height)

	if inputRow == "" {
		pane := t.renderPane(t.paneWidth, t.height)
		return t.styleProvider.JoinHorizontal(sidebar, divider, pane)
	}

	regionHeight := max(t.height-t.styleProvider.GetHeight(inputRow), 1)
	pane := t.renderPane(t.paneWidth, regionHeight)
	chatColumn := t.styleProvider.JoinVertical(pane, inputRow)
	return t.styleProvider.JoinHorizontal(sidebar, divider, chatColumn)
}

func (t *FileExplorerImpl) renderDivider(height int) string {
	line := t.styleProvider.RenderDimText("│")
	lines := make([]string, height)
	for i := range lines {
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

func (t *FileExplorerImpl) renderSidebar(width, height int) string {
	lines := t.sidebarLines(width)

	start := 0
	if len(lines) > height {
		start = clampInt(t.cursor-height/2, 0, len(lines)-height)
	}
	t.sidebarScroll = start

	blank := strings.Repeat(" ", width)
	out := make([]string, height)
	for i := range height {
		if idx := start + i; idx < len(lines) {
			out[i] = lines[idx]
		} else {
			out[i] = blank
		}
	}
	return strings.Join(out, "\n")
}

func (t *FileExplorerImpl) sidebarLines(width int) []string {
	if len(t.rows) == 0 {
		return []string{t.padPlain(t.styleProvider.RenderDimText("(empty)"), "(empty)", width)}
	}
	lines := make([]string, len(t.rows))
	for i, r := range t.rows {
		lines[i] = t.rowLine(r, width, i == t.cursor)
	}
	return lines
}

func (t *FileExplorerImpl) rowLine(r explorerRow, width int, selected bool) string {
	cursor := "  "
	if selected {
		cursor = "❯ "
	}
	indent := strings.Repeat("  ", r.depth)
	marker := "• "
	if r.node.isDir {
		marker = chevron(!r.expanded) + " "
	}
	plain := cursor + indent + marker + r.node.name
	plain = truncateRunes(plain, width)
	styled := t.styleRow(r, plain, selected)
	if pad := width - len([]rune(plain)); pad > 0 {
		styled += strings.Repeat(" ", pad)
	}
	return styled
}

func (t *FileExplorerImpl) styleRow(r explorerRow, text string, selected bool) string {
	switch {
	case selected:
		return t.styleProvider.RenderWithColorAndBold(text, t.styleProvider.GetThemeColor("accent"))
	case r.node.isDir:
		return t.styleProvider.RenderBoldText(text)
	default:
		if color := t.fileColor(r.node.name); color != "" {
			return t.styleProvider.RenderWithColor(text, color)
		}
		return t.styleProvider.RenderAssistantText(text)
	}
}

// fileColor returns a theme color key conveying a file's type (code/config/docs),
// or "" for the default text color. Coloring is alignment-safe, so it works as a
// "file-type glyph" without requiring nerd-font icons.
func (t *FileExplorerImpl) fileColor(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".go", ".js", ".jsx", ".ts", ".tsx", ".py", ".rs", ".java", ".c", ".h", ".cpp", ".rb", ".sh", ".bash":
		return t.styleProvider.GetThemeColor("accent")
	case ".json", ".yaml", ".yml", ".toml", ".ini", ".env", ".conf":
		return t.styleProvider.GetThemeColor("status")
	case ".md", ".txt", ".rst", ".adoc":
		return t.styleProvider.GetThemeColor("success")
	default:
		return ""
	}
}

func (t *FileExplorerImpl) renderPane(width, height int) string {
	switch {
	case t.editMode && t.editor != nil:
		return t.editor.View(width, height)
	case t.annotateMode:
		return t.renderAnnotatePane(width, height)
	case t.findMode:
		return t.renderFindResults(width, height)
	case t.loadErr != nil:
		return t.styleProvider.PlaceCenter(width, height, t.styleProvider.RenderErrorText(t.loadErr.Error()))
	}

	rel := t.selectedFilePath()
	if rel == "" {
		return t.styleProvider.PlaceCenter(width, height, t.styleProvider.RenderDimText("Select a file to preview"))
	}
	t.ensurePreview(rel, width)
	t.viewport.SetWidth(width)
	t.viewport.SetHeight(height)
	return t.viewport.View()
}

// renderAnnotatePane draws an inline annotation prompt at the top of the preview
// pane with the file preview (selected range highlighted) beneath it, so the
// user sees the snippet they are annotating while typing the instruction.
func (t *FileExplorerImpl) renderAnnotatePane(width, height int) string {
	rel := t.selectedFilePath()
	if rel == "" {
		return t.styleProvider.PlaceCenter(width, height, t.styleProvider.RenderDimText("No file selected"))
	}

	lo, hi, _ := t.previewSelectionRange()
	prompt := fmt.Sprintf("Annotate (%s lines %d-%d): %s", rel, lo+1, hi+1, t.annotateInput)
	header := t.styleProvider.RenderWithColorAndBold(truncateRunes(prompt, width), t.styleProvider.GetThemeColor("accent"))

	previewHeight := max(height-1, 0)
	t.ensurePreview(rel, width)
	t.viewport.SetWidth(width)
	t.viewport.SetHeight(previewHeight)

	var b strings.Builder
	b.WriteString(header)
	b.WriteByte('\n')
	b.WriteString(t.viewport.View())
	return b.String()
}

func (t *FileExplorerImpl) renderFindResults(width, height int) string {
	var b strings.Builder
	prompt := "❯ " + t.findQuery
	b.WriteString(t.styleProvider.RenderWithColorAndBold(truncateRunes(prompt, width), t.styleProvider.GetThemeColor("accent")))
	b.WriteByte('\n')

	if t.walking {
		b.WriteString(t.styleProvider.RenderDimText("indexing…"))
		return b.String()
	}

	status := fmt.Sprintf("%d of %d", len(t.filtered), len(t.candidates))
	if t.walkTruncated {
		status += fmt.Sprintf(" (capped at %d)", explorerMaxFindFiles)
	}
	b.WriteString(t.styleProvider.RenderDimText(status))
	b.WriteByte('\n')

	rows := max(height-2, 0)
	for i := 0; i < len(t.filtered) && i < rows; i++ {
		b.WriteString(t.renderHit(t.filtered[i], i == t.findCursor, width))
		if i < len(t.filtered)-1 && i < rows-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (t *FileExplorerImpl) renderHit(path string, selected bool, width int) string {
	prefix := "  "
	if selected {
		prefix = "❯ "
	}
	text := truncateRunes(path, max(width-2, 1))
	if selected {
		return t.styleProvider.RenderWithColorAndBold(prefix+text, t.styleProvider.GetThemeColor("accent"))
	}
	return prefix + t.styleProvider.RenderAssistantText(text)
}

// --- small helpers ---

func (t *FileExplorerImpl) padPlain(styled, plain string, width int) string {
	if pad := width - len([]rune(plain)); pad > 0 {
		return styled + strings.Repeat(" ", pad)
	}
	return styled
}

// ignoreFilter decides which entries to hide, honoring the root .gitignore,
// per-directory nested .gitignore files, and a set of always-ignored defaults.
// It is self-contained (no shared mutable state) so it can be used both on the
// UI goroutine (the tree) and inside the fuzzy-finder walk goroutine.
type ignoreFilter struct {
	root       string
	showHidden bool
	rootIgnore *ignore.GitIgnore
	cache      map[string]*ignore.GitIgnore // dir abs path -> compiled (nil = none)
}

func newIgnoreFilter(root string, showHidden bool) *ignoreFilter {
	rootIg, err := ignore.CompileIgnoreFileAndLines(filepath.Join(root, ".gitignore"), gitignoreDefaults...)
	if err != nil {
		rootIg = ignore.CompileIgnoreLines(gitignoreDefaults...)
	}
	return &ignoreFilter{
		root:       root,
		showHidden: showHidden,
		rootIgnore: rootIg,
		cache:      make(map[string]*ignore.GitIgnore),
	}
}

// shouldHide reports whether the entry named `name` (rel = path relative to
// root; dirAbs = absolute path of the containing directory) should be hidden.
func (f *ignoreFilter) shouldHide(rel, name, dirAbs string, isDir bool) bool {
	if f.showHidden {
		return false
	}
	if strings.HasPrefix(name, ".") {
		return true
	}
	matchRel := rel
	if isDir {
		matchRel = rel + "/"
	}
	if f.rootIgnore != nil && f.rootIgnore.MatchesPath(matchRel) {
		return true
	}
	if dirIg := f.dirIgnore(dirAbs); dirIg != nil && dirIg.MatchesPath(name) {
		return true
	}
	return false
}

func (f *ignoreFilter) dirIgnore(dirAbs string) *ignore.GitIgnore {
	if cached, ok := f.cache[dirAbs]; ok {
		return cached
	}
	var gi *ignore.GitIgnore
	p := filepath.Join(dirAbs, ".gitignore")
	if _, err := os.Stat(p); err == nil {
		if compiled, err := ignore.CompileIgnoreFile(p); err == nil {
			gi = compiled
		}
	}
	f.cache[dirAbs] = gi
	return gi
}

// walkProject walks the tree under root (honoring .gitignore unless showHidden),
// returning file paths relative to root. It stops at explorerMaxFindFiles and
// reports truncation. Pure and self-contained for safe use in a goroutine.
func walkProject(root string, showHidden bool) ([]string, bool) {
	f := newIgnoreFilter(root, showHidden)
	var paths []string
	truncated := false
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || p == root {
			return nil //nolint:nilerr // skip unreadable entries rather than abort
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return nil
		}
		if f.shouldHide(rel, d.Name(), filepath.Dir(p), d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		paths = append(paths, rel)
		if len(paths) >= explorerMaxFindFiles {
			truncated = true
			return filepath.SkipAll
		}
		return nil
	})
	return paths, truncated
}
