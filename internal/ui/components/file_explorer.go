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
	if t.findMode {
		return "type to filter · ↑/↓ select · enter open · esc back to tree"
	}
	return fmt.Sprintf("%s/%s select · %s/%s expand · %s find · %s open · %s hidden · %s back",
		t.keymap.display(actExpNavUp), t.keymap.display(actExpNavDown),
		t.keymap.display(actExpExpand), t.keymap.display(actExpCollapse),
		t.keymap.display(actExpFind), t.keymap.display(actExpOpen),
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
		actExpOpen, actExpFind, actExpToggleHidden,
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
// read + highlight only run when the selection or width actually changed.
func (t *FileExplorerImpl) ensurePreview(rel string, width int) {
	if !t.dirtyPreview && rel == t.previewKey && width == t.previewWidth {
		return
	}
	content := t.computePreview(rel)
	changed := rel != t.previewKey
	t.previewKey = rel
	t.previewWidth = width
	t.dirtyPreview = false
	t.viewport.SetContent(content)
	if changed {
		t.viewport.GotoTop()
	}
}

func (t *FileExplorerImpl) computePreview(rel string) string {
	abs := filepath.Join(t.root, rel)
	info, err := os.Stat(abs)
	if err != nil {
		return t.styleProvider.RenderErrorText("Failed to read file: " + err.Error())
	}
	if info.Size() > explorerMaxPreviewBytes {
		return t.styleProvider.RenderDimText("⊘ Binary or large file - not shown")
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return t.styleProvider.RenderErrorText("Failed to read file: " + err.Error())
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return t.styleProvider.RenderDimText("⊘ Binary or large file - not shown")
	}
	return diffview.Highlight(rel, string(data), t.chromaStyle(), true)
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
