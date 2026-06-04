package components

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	viewport "charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	gitdiff "github.com/inference-gateway/cli/internal/services/gitdiff"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// diffRefreshInterval is how often the changes panel re-polls git while open,
// so edits from the agent, an editor, or anywhere else show up "live".
const diffRefreshInterval = time.Second

const (
	diffSidebarMinWidth = 24
	diffSidebarMaxWidth = 44
	diffMinPaneWidth    = 20
)

// Configurable action IDs for the changes panel (defaults in
// config.addDiffViewerBindings; users override them in keybindings.yaml).
var (
	actDiffNavUp      = config.ActionID(config.NamespaceDiffViewer, "nav_up")
	actDiffNavDown    = config.ActionID(config.NamespaceDiffViewer, "nav_down")
	actDiffCollapse   = config.ActionID(config.NamespaceDiffViewer, "collapse")
	actDiffExpand     = config.ActionID(config.NamespaceDiffViewer, "expand")
	actDiffToggle     = config.ActionID(config.NamespaceDiffViewer, "toggle")
	actDiffStage      = config.ActionID(config.NamespaceDiffViewer, "stage")
	actDiffUnstage    = config.ActionID(config.NamespaceDiffViewer, "unstage")
	actDiffDiscard    = config.ActionID(config.NamespaceDiffViewer, "discard")
	actDiffPatch      = config.ActionID(config.NamespaceDiffViewer, "patch")
	actDiffEdit       = config.ActionID(config.NamespaceDiffViewer, "edit")
	actDiffCommit     = config.ActionID(config.NamespaceDiffViewer, "commit")
	actDiffScrollUp   = config.ActionID(config.NamespaceDiffViewer, "scroll_up")
	actDiffScrollDown = config.ActionID(config.NamespaceDiffViewer, "scroll_down")
	actDiffHalfUp     = config.ActionID(config.NamespaceDiffViewer, "halfpage_up")
	actDiffHalfDown   = config.ActionID(config.NamespaceDiffViewer, "halfpage_down")
	actDiffPatchApply = config.ActionID(config.NamespaceDiffViewer, "patch_apply")
	actDiffCancel     = config.ActionID(config.NamespaceDiffViewer, "cancel")
)

// diffKeymap resolves pressed keys to configurable diff-panel action IDs and
// exposes each action's primary key for footer hints.
type diffKeymap struct {
	keys map[string][]string // actionID -> effective keys
}

// match returns the first of the candidate action IDs bound to the pressed key.
func (k diffKeymap) match(pressed string, candidates ...string) string {
	for _, id := range candidates {
		if slices.Contains(k.keys[id], pressed) {
			return id
		}
	}
	return ""
}

// display returns the primary (first) key bound to an action, for hint text.
func (k diffKeymap) display(actionID string) string {
	if ks := k.keys[actionID]; len(ks) > 0 {
		return ks[0]
	}
	return ""
}

// normalizeKey maps the raw space key (" ") to the "space" token used in config.
func normalizeKey(s string) string {
	if s == " " {
		return "space"
	}
	return s
}

// rowKind classifies a visible row in the changes tree.
type rowKind int

const (
	rowSection rowKind = iota
	rowFolder
	rowFile
)

// diffRow is one rendered line of the tree (flattened from the staged/unstaged
// groups honoring per-node collapse state).
type diffRow struct {
	kind        rowKind
	label       string
	indent      int
	collapseKey string
	collapsed   bool
	count       int
	fc          gitdiff.FileChange
}

// diffViewerLoadedMsg carries the result of an async git status query.
type diffViewerLoadedMsg struct {
	staged   []gitdiff.FileChange
	unstaged []gitdiff.FileChange
	err      error
}

// diffViewerTickMsg drives the periodic live refresh.
type diffViewerTickMsg struct{}

// patchLoadedMsg carries the file patch loaded when entering patch (hunk) mode.
type patchLoadedMsg struct {
	patch  gitdiff.FilePatch
	path   string
	staged bool
	err    error
}

// patchReloadedMsg carries the refreshed patch after a hunk was applied.
type patchReloadedMsg struct {
	patch gitdiff.FilePatch
	err   error
}

// patchErrMsg reports a patch-mode problem as a transient message.
type patchErrMsg struct{ err error }

// DiffViewerImpl is the VS Code-style "Changes" side panel: a left tree of
// changed files (grouped Staged/Changes → folder → file) plus a scrollable diff
// pane for the selected file. It owns its full [sidebar | divider | diff]
// region; the chat input row is composed beneath the diff pane by the caller.
type DiffViewerImpl struct {
	source        gitdiff.Source
	styleProvider *styles.Provider
	themeService  domain.ThemeService
	diffRenderer  *DiffRenderer
	keymap        diffKeymap

	width        int
	height       int
	sidebarWidth int
	paneWidth    int

	staged    []gitdiff.FileChange
	unstaged  []gitdiff.FileChange
	rows      []diffRow
	collapsed map[string]bool

	cursor        int
	selectedKey   string
	sidebarScroll int

	viewport    viewport.Model
	diffContent string
	diffPath    string
	diffWidth   int
	dirtyDiff   bool
	resetScroll bool

	// Patch (hunk-staging) mode — entered with `p` on a file.
	patchMode    bool
	patchFile    gitdiff.FilePatch
	patchPath    string
	patchStaged  bool
	patchHunk    int
	patchContent string
	hunkOffsets  []int
	patchMsg     string

	// Edit mode — entered with `v`: the user's real editor runs in a PTY,
	// rendered into the pane (see pty_editor.go).
	editMode bool
	editor   *ptyEditor

	// confirmDiscard holds the file awaiting a y/n discard confirmation.
	confirmDiscard *gitdiff.FileChange

	loading bool
	loadErr error
	done    bool
	cancel  bool
}

// NewDiffViewer creates a changes panel backed by the given git source.
func NewDiffViewer(source gitdiff.Source, styleProvider *styles.Provider, themeService domain.ThemeService, kb config.KeybindingsConfig) *DiffViewerImpl {
	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	vp.SetContent("")
	vp.MouseWheelEnabled = true
	vp.MouseWheelDelta = 3

	return &DiffViewerImpl{
		source:        source,
		styleProvider: styleProvider,
		themeService:  themeService,
		diffRenderer:  NewDiffRenderer(styleProvider),
		keymap:        diffKeymap{keys: config.ResolveNamespaceBindings(kb, config.NamespaceDiffViewer)},
		collapsed:     make(map[string]bool),
		viewport:      vp,
		loading:       true,
		dirtyDiff:     true,
	}
}

func (t *DiffViewerImpl) Init() tea.Cmd {
	t.loading = true
	return tea.Batch(t.loadCmd(), t.tickCmd())
}

// Reset clears state so the panel can be reused on a later open.
func (t *DiffViewerImpl) Reset() {
	t.done = false
	t.cancel = false
	t.loading = true
	t.loadErr = nil
	t.staged = nil
	t.unstaged = nil
	t.rows = nil
	t.collapsed = make(map[string]bool)
	t.cursor = 0
	t.selectedKey = ""
	t.sidebarScroll = 0
	t.diffContent = ""
	t.diffPath = ""
	t.dirtyDiff = true
	t.resetScroll = true
	t.patchMode = false
	t.patchFile = gitdiff.FilePatch{}
	t.patchPath = ""
	t.patchHunk = 0
	t.patchContent = ""
	t.patchMsg = ""
	if t.editor != nil {
		t.editor.close()
	}
	t.editMode = false
	t.editor = nil
	t.confirmDiscard = nil
}

func (t *DiffViewerImpl) IsDone() bool      { return t.done }
func (t *DiffViewerImpl) IsCancelled() bool { return t.cancel }

// HintText returns the footer hint for the current mode (tree vs patch).
func (t *DiffViewerImpl) HintText() string {
	if t.editMode && t.editor != nil {
		return "(vim) — :wq to save & return"
	}
	if t.confirmDiscard != nil {
		return "discard " + t.confirmDiscard.Path + "?  y confirm · n cancel"
	}
	if t.patchMode {
		verb := "stage hunk"
		if t.patchStaged {
			verb = "unstage hunk"
		}
		return fmt.Sprintf("%s/%s hunk · %s %s · %s back",
			t.keymap.display(actDiffNavUp), t.keymap.display(actDiffNavDown),
			t.keymap.display(actDiffPatchApply), verb, t.keymap.display(actDiffCancel))
	}
	return fmt.Sprintf("%s/%s select · %s stage · %s unstage · %s discard · %s patch · %s edit · %s commit · %s back",
		t.keymap.display(actDiffNavUp), t.keymap.display(actDiffNavDown),
		t.keymap.display(actDiffStage), t.keymap.display(actDiffUnstage),
		t.keymap.display(actDiffDiscard), t.keymap.display(actDiffPatch),
		t.keymap.display(actDiffEdit), t.keymap.display(actDiffCommit),
		t.keymap.display(actDiffCancel))
}

// PaneWidth returns the current diff-pane width (after SetWidth), so the caller
// can size the input row that sits beneath the diff pane.
func (t *DiffViewerImpl) PaneWidth() int { return t.paneWidth }

func (t *DiffViewerImpl) SetWidth(w int) {
	t.width = w
	sidebar := clampInt(w*30/100, diffSidebarMinWidth, diffSidebarMaxWidth)
	if sidebar > w-diffMinPaneWidth {
		sidebar = max(w-diffMinPaneWidth, 1)
	}
	t.sidebarWidth = sidebar
	t.paneWidth = max(w-sidebar-1, 1)
}

func (t *DiffViewerImpl) SetHeight(h int) { t.height = h }

// --- update ---

func (t *DiffViewerImpl) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if t.editMode {
		return t.updateEditor(msg)
	}
	switch m := msg.(type) {
	case diffViewerLoadedMsg:
		return t.handleLoaded(m)
	case patchLoadedMsg:
		return t.handlePatchLoaded(m)
	case patchReloadedMsg:
		return t.handlePatchReloaded(m)
	case patchErrMsg:
		t.patchMode = false
		t.patchMsg = m.err.Error()
		t.dirtyDiff = true
		return t, nil
	case diffViewerTickMsg:
		return t, tea.Batch(t.loadCmd(), t.tickCmd())
	case tea.WindowSizeMsg:
		t.SetWidth(m.Width)
		t.SetHeight(m.Height)
		t.dirtyDiff = true
		return t, nil
	case tea.MouseWheelMsg:
		t.handleWheel(m)
		return t, nil
	case tea.KeyMsg:
		if t.loading {
			return t, nil
		}
		return t.handleKey(m)
	}
	return t, nil
}

func (t *DiffViewerImpl) handleLoaded(msg diffViewerLoadedMsg) (tea.Model, tea.Cmd) {
	t.loading = false
	t.loadErr = msg.err
	if msg.err == nil {
		t.staged = msg.staged
		t.unstaged = msg.unstaged
		t.rebuildRows()
		t.reanchorSelection()
		t.dirtyDiff = true
	}
	return t, nil
}

func (t *DiffViewerImpl) handleWheel(msg tea.MouseWheelMsg) {
	switch msg.Button {
	case tea.MouseWheelUp:
		t.viewport.ScrollUp(t.viewport.MouseWheelDelta)
	case tea.MouseWheelDown:
		t.viewport.ScrollDown(t.viewport.MouseWheelDelta)
	}
}

func (t *DiffViewerImpl) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if t.patchMode {
		return t.handlePatchKey(msg)
	}
	pressed := normalizeKey(msg.String())
	if t.confirmDiscard != nil {
		return t.handleDiscardConfirm(pressed)
	}
	if pressed == "ctrl+c" { // universal escape; intentionally not remappable
		t.cancel = true
		return t, nil
	}
	switch t.keymap.match(pressed,
		actDiffNavUp, actDiffNavDown, actDiffToggle, actDiffExpand, actDiffCollapse,
		actDiffStage, actDiffUnstage, actDiffDiscard, actDiffPatch, actDiffEdit, actDiffCommit,
		actDiffScrollUp, actDiffScrollDown, actDiffHalfUp, actDiffHalfDown, actDiffCancel) {
	case actDiffCancel:
		t.cancel = true
	case actDiffNavUp:
		t.moveCursor(-1)
	case actDiffNavDown:
		t.moveCursor(1)
	case actDiffToggle:
		return t.toggleOrSelect()
	case actDiffExpand:
		t.setCollapsed(false)
	case actDiffCollapse:
		t.setCollapsed(true)
	case actDiffScrollUp:
		t.viewport.ScrollUp(10)
	case actDiffScrollDown:
		t.viewport.ScrollDown(10)
	case actDiffHalfUp:
		t.viewport.ScrollUp(5)
	case actDiffHalfDown:
		t.viewport.ScrollDown(5)
	case actDiffStage:
		return t, t.stageCmd()
	case actDiffUnstage:
		return t, t.unstageCmd()
	case actDiffDiscard:
		if fc := t.selectedFile(); fc != nil {
			t.confirmDiscard = fc
		}
	case actDiffPatch:
		return t, t.enterPatchCmd()
	case actDiffEdit:
		return t, t.enterEditCmd()
	case actDiffCommit:
		return t.commit()
	}
	return t, nil
}

func (t *DiffViewerImpl) moveCursor(delta int) {
	if len(t.rows) == 0 {
		return
	}
	t.patchMsg = ""
	prev := t.selectedFilePath()
	t.cursor = clampInt(t.cursor+delta, 0, len(t.rows)-1)
	t.selectedKey = t.rowKey(t.rows[t.cursor])
	if t.selectedFilePath() != prev {
		t.dirtyDiff = true
		t.resetScroll = true
	}
}

func (t *DiffViewerImpl) toggleOrSelect() (tea.Model, tea.Cmd) {
	if t.cursor < 0 || t.cursor >= len(t.rows) {
		return t, nil
	}
	row := t.rows[t.cursor]
	if row.kind == rowFile {
		t.dirtyDiff = true
		return t, nil
	}
	t.collapsed[row.collapseKey] = !t.collapsed[row.collapseKey]
	t.rebuildRows()
	t.reanchorSelection()
	return t, nil
}

func (t *DiffViewerImpl) setCollapsed(collapsed bool) {
	if t.cursor < 0 || t.cursor >= len(t.rows) {
		return
	}
	row := t.rows[t.cursor]
	if row.kind == rowFile {
		return
	}
	t.collapsed[row.collapseKey] = collapsed
	t.rebuildRows()
	t.reanchorSelection()
}

func (t *DiffViewerImpl) stageCmd() tea.Cmd {
	fc := t.selectedFile()
	if fc == nil {
		return nil
	}
	path := fc.Path
	src := t.source
	return func() tea.Msg {
		if err := src.Stage(path); err != nil {
			return diffViewerLoadedMsg{err: err}
		}
		staged, unstaged, err := src.Changes()
		return diffViewerLoadedMsg{staged: staged, unstaged: unstaged, err: err}
	}
}

func (t *DiffViewerImpl) unstageCmd() tea.Cmd {
	fc := t.selectedFile()
	if fc == nil {
		return nil
	}
	path := fc.Path
	src := t.source
	return func() tea.Msg {
		if err := src.Unstage(path); err != nil {
			return diffViewerLoadedMsg{err: err}
		}
		staged, unstaged, err := src.Changes()
		return diffViewerLoadedMsg{staged: staged, unstaged: unstaged, err: err}
	}
}

// discardCmd discards the file's working-tree changes, then reloads the tree.
func (t *DiffViewerImpl) discardCmd(fc gitdiff.FileChange) tea.Cmd {
	src := t.source
	return func() tea.Msg {
		if err := src.Discard(fc); err != nil {
			return diffViewerLoadedMsg{err: err}
		}
		staged, unstaged, err := src.Changes()
		return diffViewerLoadedMsg{staged: staged, unstaged: unstaged, err: err}
	}
}

// handleDiscardConfirm resolves the pending discard confirmation: `y` discards
// the file's working-tree changes; any other key cancels.
func (t *DiffViewerImpl) handleDiscardConfirm(key string) (tea.Model, tea.Cmd) {
	fc := t.confirmDiscard
	t.confirmDiscard = nil
	t.dirtyDiff = true
	if key == "y" && fc != nil {
		return t, t.discardCmd(*fc)
	}
	return t, nil
}

// commit closes the panel and replays the existing `/git commit` flow, which
// generates an AI commit message for the staged changes and drops the resulting
// `!git commit -m "..."` into the input for the user to confirm.
func (t *DiffViewerImpl) commit() (tea.Model, tea.Cmd) {
	t.cancel = true
	return t, func() tea.Msg {
		return domain.UserInputEvent{Content: "/git commit"}
	}
}

// --- edit mode (real editor in a PTY) ---

// enterEditCmd launches the user's editor ($VISUAL/$EDITOR/vim) on the selected
// file in a PTY rendered into the pane, skipping deleted files. The returned cmd
// streams the editor's terminal output back as ptyOutputMsg/ptyExitMsg.
func (t *DiffViewerImpl) enterEditCmd() tea.Cmd {
	fc := t.selectedFile()
	if fc == nil || fc.Status == gitdiff.StatusDeleted {
		return nil
	}
	abs := filepath.Join(t.source.Workdir(), fc.Path)
	editor, readCmd, err := startPTYEditor(abs, t.source.Workdir(), t.paneWidth, max(t.height-1, 1))
	if err != nil {
		t.patchMsg = "Failed to open editor: " + err.Error()
		return nil
	}
	t.editor = editor
	t.editMode = true
	return readCmd
}

// updateEditor drives the embedded editor: it forwards keys to the PTY, feeds
// PTY output into the emulator (re-arming the reader), and on child exit closes
// the editor and refreshes the tree/diff so the change shows immediately.
func (t *DiffViewerImpl) updateEditor(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case ptyOutputMsg:
		t.editor.term.write(m.data)
		return t, t.editor.readCmd()
	case ptyExitMsg:
		t.editor.close()
		t.editor = nil
		t.editMode = false
		t.dirtyDiff = true
		return t, t.loadCmd()
	case tea.KeyPressMsg:
		t.editor.write(encodeKey(m))
		return t, nil
	case diffViewerTickMsg:
		// Keep the live-refresh tick chain alive while editing.
		return t, t.tickCmd()
	case tea.WindowSizeMsg:
		t.SetWidth(m.Width)
		t.SetHeight(m.Height)
	}
	return t, nil
}

// --- patch (hunk staging) mode ---

// handlePatchKey handles keys while in patch mode: hunk navigation, applying the
// current hunk (a/s stage a worktree hunk, u unstage a staged hunk — the
// direction follows whichever patch is loaded), scrolling, and esc to exit.
func (t *DiffViewerImpl) handlePatchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch t.keymap.match(normalizeKey(msg.String()),
		actDiffCancel, actDiffNavUp, actDiffNavDown, actDiffPatchApply,
		actDiffScrollUp, actDiffScrollDown, actDiffHalfUp, actDiffHalfDown) {
	case actDiffCancel:
		t.patchMode = false
		t.dirtyDiff = true
	case actDiffNavUp:
		t.movePatchHunk(-1)
	case actDiffNavDown:
		t.movePatchHunk(1)
	case actDiffPatchApply:
		return t, t.applyHunkCmd()
	case actDiffScrollUp:
		t.viewport.ScrollUp(10)
	case actDiffScrollDown:
		t.viewport.ScrollDown(10)
	case actDiffHalfUp:
		t.viewport.ScrollUp(5)
	case actDiffHalfDown:
		t.viewport.ScrollDown(5)
	}
	return t, nil
}

// enterPatchCmd loads the selected file's patch (worktree for an unstaged file,
// index for a staged one) so its hunks can be staged/unstaged individually.
func (t *DiffViewerImpl) enterPatchCmd() tea.Cmd {
	fc := t.selectedFile()
	if fc == nil {
		return nil
	}
	if fc.Status == gitdiff.StatusUntracked {
		return func() tea.Msg {
			return patchErrMsg{err: fmt.Errorf("untracked file — press a to stage it whole")}
		}
	}
	path, staged, src := fc.Path, fc.Staged, t.source
	return func() tea.Msg {
		var fp gitdiff.FilePatch
		var err error
		if staged {
			fp, err = src.IndexPatch(path)
		} else {
			fp, err = src.WorktreePatch(path)
		}
		return patchLoadedMsg{patch: fp, path: path, staged: staged, err: err}
	}
}

// applyHunkCmd applies the current hunk to the index, then reloads the patch and
// the file tree. Direction follows the loaded patch: stage a worktree hunk, or
// unstage (reverse) a staged hunk.
func (t *DiffViewerImpl) applyHunkCmd() tea.Cmd {
	if !t.patchMode || len(t.patchFile.Hunks) == 0 {
		return nil
	}
	fp, idx, path, staged, src := t.patchFile, t.patchHunk, t.patchPath, t.patchStaged, t.source
	return func() tea.Msg {
		if err := src.ApplyHunk(fp, idx, staged); err != nil {
			return patchErrMsg{err: err}
		}
		var np gitdiff.FilePatch
		var err error
		if staged {
			np, err = src.IndexPatch(path)
		} else {
			np, err = src.WorktreePatch(path)
		}
		return patchReloadedMsg{patch: np, err: err}
	}
}

func (t *DiffViewerImpl) handlePatchLoaded(msg patchLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		t.patchMsg = "Failed to load patch: " + msg.err.Error()
		return t, nil
	}
	if len(msg.patch.Hunks) == 0 {
		t.patchMsg = "No hunks to stage for this file"
		return t, nil
	}
	t.patchMode = true
	t.patchFile = msg.patch
	t.patchPath = msg.path
	t.patchStaged = msg.staged
	t.patchHunk = 0
	t.patchMsg = ""
	t.rebuildPatchContent()
	t.viewport.GotoTop()
	return t, nil
}

func (t *DiffViewerImpl) handlePatchReloaded(msg patchReloadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		t.patchMode = false
		t.patchMsg = msg.err.Error()
		t.dirtyDiff = true
		return t, t.loadCmd()
	}
	t.patchFile = msg.patch
	if len(msg.patch.Hunks) == 0 {
		// Whole file staged/unstaged — leave patch mode and refresh the tree.
		t.patchMode = false
		t.dirtyDiff = true
		return t, t.loadCmd()
	}
	t.patchHunk = clampInt(t.patchHunk, 0, len(msg.patch.Hunks)-1)
	t.rebuildPatchContent()
	return t, t.loadCmd()
}

func (t *DiffViewerImpl) movePatchHunk(delta int) {
	if len(t.patchFile.Hunks) == 0 {
		return
	}
	t.patchHunk = clampInt(t.patchHunk+delta, 0, len(t.patchFile.Hunks)-1)
	t.rebuildPatchContent()
	t.viewport.GotoTop()
	if t.patchHunk < len(t.hunkOffsets) {
		t.viewport.ScrollDown(t.hunkOffsets[t.patchHunk])
	}
}

// rebuildPatchContent renders the full patch (all hunks) into the viewport,
// marking the active hunk with a colored left bar, and records each hunk's start
// line so navigation can scroll to it.
func (t *DiffViewerImpl) rebuildPatchContent() {
	t.hunkOffsets = t.hunkOffsets[:0]
	accent := t.styleProvider.GetThemeColor("accent")
	activeBar := t.styleProvider.RenderWithColor("▌ ", accent)

	var b strings.Builder
	line := 0
	for i, h := range t.patchFile.Hunks {
		t.hunkOffsets = append(t.hunkOffsets, line)
		bar := "  "
		if i == t.patchHunk {
			bar = activeBar
		}
		b.WriteString(bar)
		b.WriteString(t.styleProvider.RenderWithColorAndBold(h.Header, accent))
		b.WriteByte('\n')
		line++
		for _, l := range h.Lines {
			b.WriteString(bar)
			b.WriteString(t.colorPatchLine(l))
			b.WriteByte('\n')
			line++
		}
	}
	t.patchContent = strings.TrimRight(b.String(), "\n")
	t.viewport.SetContent(t.patchContent)
}

func (t *DiffViewerImpl) colorPatchLine(l string) string {
	if l == "" {
		return ""
	}
	switch l[0] {
	case '+':
		return t.styleProvider.RenderWithColor(l, t.styleProvider.GetThemeColor("diffAdd"))
	case '-':
		return t.styleProvider.RenderWithColor(l, t.styleProvider.GetThemeColor("diffRemove"))
	case '\\':
		return t.styleProvider.RenderDimText(l)
	default:
		return l
	}
}

func (t *DiffViewerImpl) renderPatch(width, height int) string {
	t.viewport.SetWidth(width)
	t.viewport.SetHeight(height)
	return t.viewport.View()
}

func (t *DiffViewerImpl) loadCmd() tea.Cmd {
	src := t.source
	return func() tea.Msg {
		staged, unstaged, err := src.Changes()
		return diffViewerLoadedMsg{staged: staged, unstaged: unstaged, err: err}
	}
}

func (t *DiffViewerImpl) tickCmd() tea.Cmd {
	return tea.Tick(diffRefreshInterval, func(_ time.Time) tea.Msg {
		return diffViewerTickMsg{}
	})
}

// --- tree model ---

func (t *DiffViewerImpl) rebuildRows() {
	t.rows = t.rows[:0]
	t.addSection("Staged Changes", "staged", t.staged)
	t.addSection("Changes", "unstaged", t.unstaged)
}

func (t *DiffViewerImpl) addSection(title, key string, files []gitdiff.FileChange) {
	if len(files) == 0 {
		return
	}
	sectionKey := "section:" + key
	collapsed := t.collapsed[sectionKey]
	t.rows = append(t.rows, diffRow{
		kind: rowSection, label: title, count: len(files),
		collapseKey: sectionKey, collapsed: collapsed,
	})
	if collapsed {
		return
	}

	for _, grp := range groupByDir(files) {
		fileIndent := 1
		if grp.dir != "" {
			folderKey := "folder:" + key + ":" + grp.dir
			folderCollapsed := t.collapsed[folderKey]
			t.rows = append(t.rows, diffRow{
				kind: rowFolder, label: grp.dir, indent: 1,
				collapseKey: folderKey, collapsed: folderCollapsed,
			})
			if folderCollapsed {
				continue
			}
			fileIndent = 2
		}
		for _, fc := range grp.files {
			t.rows = append(t.rows, diffRow{
				kind: rowFile, label: filepath.Base(fc.Path), indent: fileIndent, fc: fc,
			})
		}
	}
}

type dirGroup struct {
	dir   string
	files []gitdiff.FileChange
}

// groupByDir buckets files by parent directory, preserving first-seen order.
func groupByDir(files []gitdiff.FileChange) []dirGroup {
	order := make([]string, 0)
	byDir := make(map[string]*dirGroup)
	for _, fc := range files {
		dir := filepath.Dir(fc.Path)
		if dir == "." {
			dir = ""
		}
		grp, ok := byDir[dir]
		if !ok {
			grp = &dirGroup{dir: dir}
			byDir[dir] = grp
			order = append(order, dir)
		}
		grp.files = append(grp.files, fc)
	}
	out := make([]dirGroup, 0, len(order))
	for _, dir := range order {
		out = append(out, *byDir[dir])
	}
	return out
}

func (t *DiffViewerImpl) reanchorSelection() {
	if len(t.rows) == 0 {
		t.cursor = 0
		t.selectedKey = ""
		return
	}
	if i, ok := t.findSelectionIndex(); ok {
		t.cursor = i
	} else {
		t.cursor = clampInt(t.cursor, 0, len(t.rows)-1)
	}
	t.selectedKey = t.rowKey(t.rows[t.cursor])
}

// findSelectionIndex resolves which row to keep selected after a rebuild: the
// exact previous key, else the same file path in either group (e.g. after
// staging moves it), else the first file row so a diff shows on open.
func (t *DiffViewerImpl) findSelectionIndex() (int, bool) {
	if t.selectedKey == "" {
		return t.firstFileRow()
	}
	if i, ok := t.indexOfKey(t.selectedKey); ok {
		return i, true
	}
	if path, ok := filePathFromKey(t.selectedKey); ok {
		return t.indexOfFilePath(path)
	}
	return 0, false
}

func (t *DiffViewerImpl) indexOfKey(key string) (int, bool) {
	for i, r := range t.rows {
		if t.rowKey(r) == key {
			return i, true
		}
	}
	return 0, false
}

func (t *DiffViewerImpl) indexOfFilePath(path string) (int, bool) {
	for i, r := range t.rows {
		if r.kind == rowFile && r.fc.Path == path {
			return i, true
		}
	}
	return 0, false
}

func (t *DiffViewerImpl) firstFileRow() (int, bool) {
	for i, r := range t.rows {
		if r.kind == rowFile {
			return i, true
		}
	}
	return 0, false
}

func (t *DiffViewerImpl) rowKey(r diffRow) string {
	if r.kind == rowFile {
		return diffKey(r.fc)
	}
	return r.collapseKey
}

func (t *DiffViewerImpl) selectedFile() *gitdiff.FileChange {
	if t.cursor < 0 || t.cursor >= len(t.rows) {
		return nil
	}
	if r := t.rows[t.cursor]; r.kind == rowFile {
		fc := r.fc
		return &fc
	}
	return nil
}

func (t *DiffViewerImpl) selectedFilePath() string {
	if fc := t.selectedFile(); fc != nil {
		return fc.Path
	}
	return ""
}

func (t *DiffViewerImpl) hasAnyFile() bool {
	return len(t.staged)+len(t.unstaged) > 0
}

func diffKey(fc gitdiff.FileChange) string {
	return fmt.Sprintf("file:%t:%s", fc.Staged, fc.Path)
}

func filePathFromKey(key string) (string, bool) {
	const prefix = "file:"
	if !strings.HasPrefix(key, prefix) {
		return "", false
	}
	rest := key[len(prefix):]
	if _, path, ok := strings.Cut(rest, ":"); ok {
		return path, true
	}
	return "", false
}

// --- rendering ---

// View satisfies tea.Model. The app composes the real layout via Render (which
// stacks the input beneath the diff pane); this is a standalone fallback.
func (t *DiffViewerImpl) View() tea.View {
	return tea.NewView(t.Render(""))
}

// Render lays out the full region: a full-height sidebar and divider on the
// left, and on the right the diff pane with the (already-rendered) input row
// stacked beneath it — so the input visibly shifts right of the sidebar. Pass
// "" for inputRow to render the diff pane at full height (no input).
func (t *DiffViewerImpl) Render(inputRow string) string {
	if t.width <= 0 || t.height <= 0 {
		return ""
	}
	sidebar := t.renderSidebar(t.sidebarWidth, t.height)
	divider := t.renderDivider(t.height)

	if inputRow == "" {
		diffPane := t.renderDiffPane(t.paneWidth, t.height)
		return t.styleProvider.JoinHorizontal(sidebar, divider, diffPane)
	}

	regionHeight := max(t.height-t.styleProvider.GetHeight(inputRow), 1)
	diffPane := t.renderDiffPane(t.paneWidth, regionHeight)
	chatColumn := t.styleProvider.JoinVertical(diffPane, inputRow)
	return t.styleProvider.JoinHorizontal(sidebar, divider, chatColumn)
}

func (t *DiffViewerImpl) renderDivider(height int) string {
	line := t.styleProvider.RenderDimText("│")
	lines := make([]string, height)
	for i := range lines {
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

func (t *DiffViewerImpl) renderSidebar(width, height int) string {
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

func (t *DiffViewerImpl) sidebarLines(width int) []string {
	if t.loading {
		return []string{t.padPlain(t.styleProvider.RenderDimText("Loading changes…"), "Loading changes…", width)}
	}
	if !t.hasAnyFile() {
		return []string{t.padPlain(t.styleProvider.RenderSuccessText("✓ No changes"), "✓ No changes", width)}
	}
	lines := make([]string, len(t.rows))
	for i, r := range t.rows {
		lines[i] = t.rowLine(r, width, i == t.cursor)
	}
	return lines
}

func (t *DiffViewerImpl) rowLine(r diffRow, width int, selected bool) string {
	leftPlain, badgePlain := rowText(r)
	if selected {
		leftPlain = "❯ " + leftPlain
	} else {
		leftPlain = "  " + leftPlain
	}

	badgeW := len([]rune(badgePlain))
	avail := max(width-badgeW-1, 1)
	leftPlain = truncateRunes(leftPlain, avail)

	gap := max(width-len([]rune(leftPlain))-badgeW, 1)
	left := t.styleLeft(r, leftPlain, selected)
	if badgePlain == "" {
		return left + strings.Repeat(" ", max(width-len([]rune(leftPlain)), 0))
	}
	return left + strings.Repeat(" ", gap) + t.styleBadge(r, badgePlain)
}

// rowText returns the plain (unstyled) left label and right badge for a row.
func rowText(r diffRow) (left, badge string) {
	indent := strings.Repeat("  ", r.indent)
	switch r.kind {
	case rowSection:
		return indent + chevron(r.collapsed) + " " + r.label, fmt.Sprintf("%d", r.count)
	case rowFolder:
		return indent + chevron(r.collapsed) + " " + r.label, ""
	default: // rowFile
		// Two-space placeholder where a chevron would be, so file labels sit one
		// level deeper than their (chevron-prefixed) folder header.
		return indent + "  " + r.label, string(statusLetter(r.fc.Status))
	}
}

func (t *DiffViewerImpl) styleLeft(r diffRow, text string, selected bool) string {
	switch {
	case selected:
		return t.styleProvider.RenderWithColorAndBold(text, t.styleProvider.GetThemeColor("accent"))
	case r.kind == rowSection:
		return t.styleProvider.RenderBoldText(text)
	case r.kind == rowFolder:
		return t.styleProvider.RenderDimText(text)
	default:
		return t.styleProvider.RenderAssistantText(text)
	}
}

func (t *DiffViewerImpl) styleBadge(r diffRow, badge string) string {
	if r.kind != rowFile {
		return t.styleProvider.RenderDimText(badge)
	}
	return t.styleProvider.RenderWithColor(badge, t.statusColor(r.fc.Status))
}

func (t *DiffViewerImpl) statusColor(s gitdiff.Status) string {
	switch s {
	case gitdiff.StatusAdded, gitdiff.StatusUntracked:
		return t.styleProvider.GetThemeColor("success")
	case gitdiff.StatusDeleted:
		return t.styleProvider.GetThemeColor("error")
	case gitdiff.StatusModified, gitdiff.StatusTypeChange:
		return t.styleProvider.GetThemeColor("status")
	default:
		return t.styleProvider.GetThemeColor("accent")
	}
}

func (t *DiffViewerImpl) renderDiffPane(width, height int) string {
	switch {
	case t.editMode && t.editor != nil:
		return t.editor.View(width, height)
	case t.confirmDiscard != nil:
		prompt := "Discard changes to " + t.confirmDiscard.Path + "?  (y / n)"
		return t.styleProvider.PlaceCenter(width, height, t.styleProvider.RenderWarningText(prompt))
	case t.loading:
		return t.styleProvider.PlaceCenter(width, height, t.styleProvider.RenderDimText("Loading changes…"))
	case t.loadErr != nil:
		return t.styleProvider.PlaceCenter(width, height, t.styleProvider.RenderErrorText(t.loadErr.Error()))
	case t.patchMode:
		return t.renderPatch(width, height)
	case t.patchMsg != "":
		return t.styleProvider.PlaceCenter(width, height, t.styleProvider.RenderWarningText(t.patchMsg))
	}

	fc := t.selectedFile()
	if fc == nil {
		msg := "No changes"
		if t.hasAnyFile() {
			msg = "Select a file to view its diff"
		}
		return t.styleProvider.PlaceCenter(width, height, t.styleProvider.RenderDimText(msg))
	}

	t.ensureDiff(*fc, width)
	t.viewport.SetWidth(width)
	t.viewport.SetHeight(height)
	return t.viewport.View()
}

// ensureDiff (re)renders the selected file's diff into the viewport, gated so
// the git query + diff render only run when the selection, content (dirty), or
// width actually changed — not on every frame.
func (t *DiffViewerImpl) ensureDiff(fc gitdiff.FileChange, width int) {
	key := diffKey(fc)
	if !t.dirtyDiff && key == t.diffPath && width == t.diffWidth {
		return
	}

	t.diffContent = t.computeDiff(fc, width)
	changedFile := key != t.diffPath
	t.diffPath = key
	t.diffWidth = width
	t.dirtyDiff = false

	t.viewport.SetContent(t.diffContent)
	if changedFile || t.resetScroll {
		t.viewport.GotoTop()
		t.resetScroll = false
	}
}

func (t *DiffViewerImpl) computeDiff(fc gitdiff.FileChange, width int) string {
	oldContent, newContent, isBinary, err := t.source.Diff(fc)
	switch {
	case err != nil:
		return t.styleProvider.RenderErrorText("Failed to load diff: " + err.Error())
	case isBinary:
		return t.styleProvider.RenderDimText("⊘ Binary or large file — not shown")
	}

	info := DiffInfo{FilePath: fc.Path, OldContent: oldContent, NewContent: newContent}
	if fc.OrigPath != "" {
		info.Title = fc.OrigPath + " → " + fc.Path
	}
	return t.diffRenderer.SetWidth(width).RenderDiff(info)
}

// --- small helpers ---

func chevron(collapsed bool) string {
	if collapsed {
		return "▸"
	}
	return "▾"
}

// statusLetter maps a git status to its display letter (untracked shows as U).
func statusLetter(s gitdiff.Status) rune {
	if s == gitdiff.StatusUntracked {
		return 'U'
	}
	return rune(s)
}

// padPlain right-pads a styled string to width using its plain-text length.
func (t *DiffViewerImpl) padPlain(styled, plain string, width int) string {
	if pad := width - len([]rune(plain)); pad > 0 {
		return styled + strings.Repeat(" ", pad)
	}
	return styled
}

func truncateRunes(s string, maxWidth int) string {
	r := []rune(s)
	if len(r) <= maxWidth {
		return s
	}
	if maxWidth <= 1 {
		return string(r[:max(maxWidth, 0)])
	}
	return string(r[:maxWidth-1]) + "…"
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
