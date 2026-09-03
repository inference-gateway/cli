package components

import (
	"fmt"
	"strings"

	key "charm.land/bubbles/v2/key"
	textinput "charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	huh "charm.land/huh/v2"
	lipgloss "charm.land/lipgloss/v2"

	config "github.com/inference-gateway/cli/config"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	models "github.com/inference-gateway/cli/internal/platform/models"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
)

// ModelViewMode defines the different filter modes for models
type ModelViewMode int

const (
	ModelViewAll ModelViewMode = iota
	ModelViewFree
	ModelViewPayAsYouGo
	ModelViewSubscription
)

// ModelCapabilityFilter narrows the list by input modality, ANDed with the
// pricing tab. Only chat-capable models are listed at all, so "text" is not a
// filter; the dimensions are the extra input modalities a chat model accepts.
type ModelCapabilityFilter int

const (
	CapabilityAny ModelCapabilityFilter = iota
	CapabilityVision
	CapabilityAudio
	CapabilityVideo
)

// modelSelectChromeLines is the vertical space around the huh select: title,
// both tab rows, separator, blank lines, and the help row.
const modelSelectChromeLines = 9

// ModelSelectorImpl implements model selection UI as a huh select with the
// pricing tabs (keys 1-4) layered on top: switching a tab rebuilds the form
// with that tab's option set. Search is a dedicated textinput (entered with
// `/`) filtering on the model name; huh's built-in filter is disabled since
// it renders the query into the select's title line instead of a real input.
type ModelSelectorImpl struct {
	models         []string
	width          int
	height         int
	styleProvider  *styles.Provider
	done           bool
	cancelled      bool
	modelService   convdomain.ModelService
	pricingService convdomain.PricingService
	config         *config.Config
	currentView    ModelViewMode
	capability     ModelCapabilityFilter

	form       *huh.Form
	sel        *huh.Select[string]
	choice     string
	search     textinput.Model
	searchMode bool
	notice     string

	titleStyle       lipgloss.Style
	tabActiveStyle   lipgloss.Style
	tabInactiveStyle lipgloss.Style
}

// NewModelSelector creates a new model selector
func NewModelSelector(models []string, modelService convdomain.ModelService, pricingService convdomain.PricingService, cfg *config.Config, styleProvider *styles.Provider) *ModelSelectorImpl {
	m := &ModelSelectorImpl{
		models:         models,
		width:          80,
		height:         24,
		styleProvider:  styleProvider,
		modelService:   modelService,
		pricingService: pricingService,
		config:         cfg,
		currentView:    ModelViewAll,
	}
	m.search = textinput.New()
	m.search.Prompt = "Search: "

	m.titleStyle = lipgloss.NewStyle().Bold(true)
	m.tabActiveStyle = lipgloss.NewStyle().Bold(true).Underline(true).Padding(0, 1)
	m.tabInactiveStyle = lipgloss.NewStyle().Padding(0, 1)
	if styleProvider != nil {
		accent := lipgloss.Color(styleProvider.GetThemeColor("accent"))
		dim := lipgloss.Color(styleProvider.GetThemeColor("dim"))
		m.titleStyle = m.titleStyle.Foreground(accent)
		m.tabActiveStyle = m.tabActiveStyle.Foreground(accent)
		m.tabInactiveStyle = m.tabInactiveStyle.Foreground(dim)
	}

	m.buildForm()
	return m
}

// buildForm (re)builds the huh select over the current tab's models. The
// form's Init cmd is discarded on purpose: the selector is routed every
// message while its view is active, so only cursor-blink cosmetics are lost.
func (m *ModelSelectorImpl) buildForm() {
	visible := m.visibleModels()
	options := make([]huh.Option[string], 0, len(visible))
	for _, model := range visible {
		label := model
		if suffix := m.formatModelSuffix(model); suffix != "" {
			label = model + " " + suffix
		}
		options = append(options, huh.NewOption(label, model))
	}

	m.notice = ""
	m.choice = ""
	m.sel = huh.NewSelect[string]().
		Title(fmt.Sprintf("%d models available", len(visible))).
		Options(options...).
		Height(m.selectHeight(len(visible))).
		Value(&m.choice)

	// huh's own / filter renders the query into the title line instead of a
	// real input, so it stays disabled in favour of the search textinput.
	keymap := huh.NewDefaultKeyMap()
	keymap.Select.Filter.SetEnabled(false)

	m.form = huh.NewForm(huh.NewGroup(m.sel)).
		WithShowHelp(false).
		WithWidth(m.width).
		WithKeyMap(keymap).
		WithTheme(huhTheme(m.styleProvider))
	_ = m.form.Init()
}

// visibleModels is the current tab's models narrowed by the search query,
// matching on the model name only (not the metadata suffix).
func (m *ModelSelectorImpl) visibleModels() []string {
	tabModels := m.tabModels()
	query := strings.ToLower(strings.TrimSpace(m.search.Value()))
	if query == "" {
		return tabModels
	}
	filtered := make([]string, 0, len(tabModels))
	for _, model := range tabModels {
		if strings.Contains(strings.ToLower(model), query) {
			filtered = append(filtered, model)
		}
	}
	return filtered
}

func (m *ModelSelectorImpl) selectHeight(optionCount int) int {
	return max(min(m.height-modelSelectChromeLines, optionCount), 3)
}

func (m *ModelSelectorImpl) Init() tea.Cmd {
	return nil
}

func (m *ModelSelectorImpl) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.buildForm()
		return m, nil
	case tea.KeyPressMsg:
		if key.Matches(msg, modelSelectorKeys.cancel) {
			m.cancelled = true
			m.done = true
			return m, tea.Quit
		}
		if m.searchMode {
			return m, m.handleSearchKey(msg)
		}
		switch {
		case key.Matches(msg, modelSelectorKeys.tab1):
			m.setPricingView(ModelViewAll)
			return m, nil
		case key.Matches(msg, modelSelectorKeys.tab2):
			m.setPricingView(ModelViewFree)
			return m, nil
		case key.Matches(msg, modelSelectorKeys.tab3):
			m.setPricingView(ModelViewPayAsYouGo)
			return m, nil
		case key.Matches(msg, modelSelectorKeys.tab4):
			m.setPricingView(ModelViewSubscription)
			return m, nil
		case key.Matches(msg, modelSelectorKeys.tab5):
			m.setCapabilityFilter(CapabilityAny)
			return m, nil
		case key.Matches(msg, modelSelectorKeys.tab6):
			m.setCapabilityFilter(CapabilityVision)
			return m, nil
		case key.Matches(msg, modelSelectorKeys.tab7):
			m.setCapabilityFilter(CapabilityAudio)
			return m, nil
		case key.Matches(msg, modelSelectorKeys.tab8):
			m.setCapabilityFilter(CapabilityVideo)
			return m, nil
		case key.Matches(msg, modelSelectorKeys.search):
			m.searchMode = true
			return m, m.search.Focus()
		}
	}

	return m, m.forwardToForm(msg)
}

// handleSearchKey routes keys while the search input is active: navigation
// and selection still reach the list, esc clears the search, and everything
// else edits the query (rebuilding the option set on change).
func (m *ModelSelectorImpl) handleSearchKey(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, modelSelectorKeys.escape):
		m.searchMode = false
		m.search.Blur()
		if m.search.Value() != "" {
			m.search.SetValue("")
			m.buildForm()
		}
		return nil
	case key.Matches(msg, modelSelectorKeys.enter),
		key.Matches(msg, modelSelectorKeys.navUp),
		key.Matches(msg, modelSelectorKeys.navDown):
		return m.forwardToForm(msg)
	}

	before := m.search.Value()
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	if m.search.Value() != before {
		m.buildForm()
	}
	return cmd
}

// forwardToForm delegates to the huh form and emits the selection event when
// it completes. A completed form with a failing SelectModel is rebuilt so the
// selector stays usable.
func (m *ModelSelectorImpl) forwardToForm(msg tea.Msg) tea.Cmd {
	model, cmd := m.form.Update(msg)
	if f, ok := model.(*huh.Form); ok {
		m.form = f
	}

	if m.form.State != huh.StateCompleted {
		return cmd
	}

	selectedModel := m.choice
	if err := m.modelService.SelectModel(selectedModel); err != nil {
		m.buildForm()
		if models.IsNonChatModel(selectedModel) {
			m.notice = fmt.Sprintf("%s does not support chat and cannot be selected", selectedModel)
		}
		return nil
	}
	m.done = true
	return func() tea.Msg {
		return tui.ModelSelectedEvent{Model: selectedModel}
	}
}

func (m *ModelSelectorImpl) setPricingView(view ModelViewMode) {
	m.currentView = view
	m.buildForm()
}

func (m *ModelSelectorImpl) setCapabilityFilter(filter ModelCapabilityFilter) {
	m.capability = filter
	m.buildForm()
}

func (m *ModelSelectorImpl) View() tea.View {
	return tea.NewView(m.viewContent())
}

func (m *ModelSelectorImpl) viewContent() string {
	var b strings.Builder

	b.WriteString(m.titleStyle.Render("Select a Model"))
	b.WriteString("\n\n")

	m.writeViewTabs(&b)

	if m.notice != "" {
		warningColor := m.styleProvider.GetThemeColor("warning")
		b.WriteString(m.styleProvider.RenderWithColor(m.notice, warningColor))
		b.WriteString("\n\n")
	}

	if m.searchMode || m.search.Value() != "" {
		b.WriteString(m.search.View())
		b.WriteString("\n\n")
	}

	if len(m.visibleModels()) == 0 {
		errorColor := m.styleProvider.GetThemeColor("error")
		if query := m.search.Value(); query != "" {
			b.WriteString(m.styleProvider.RenderWithColor(fmt.Sprintf("No models match %q", query), errorColor))
		} else {
			b.WriteString(m.styleProvider.RenderWithColor("No models available", errorColor))
		}
		b.WriteString("\n")
		return b.String()
	}

	b.WriteString(m.form.View())

	b.WriteString("\n")
	b.WriteString(m.styleProvider.RenderDimText(strings.Repeat("─", max(m.width, 1))))
	b.WriteString("\n")
	b.WriteString(m.styleProvider.RenderDimText("↑↓ navigate · Enter select · / search · esc clear · 1-4 pricing · 5-8 capability · Ctrl+C cancel"))

	return b.String()
}

// formatModelSuffix builds the parenthesised metadata shown next to each
// model row, combining the context window (compact "128K"/"1M" form, or "?"
// when no matcher pattern hits) with the pricing string when available.
func (m *ModelSelectorImpl) formatModelSuffix(model string) string {
	parts := make([]string, 0, 3)

	window, ok := models.LookupContextWindow(model)
	if ok {
		parts = append(parts, formatContextWindow(window))
	} else {
		parts = append(parts, "?")
	}

	if label := convdomain.FormatModelPricingLabel(m.pricingService, model); label != "" {
		parts = append(parts, label)
	}

	if label := models.ModalitiesLabel(model); label != "" {
		parts = append(parts, label)
	}

	if models.IsNonChatModel(model) {
		parts = append(parts, "view-only")
	}

	return fmt.Sprintf("(%s)", strings.Join(parts, ", "))
}

// formatContextWindow renders a token count as "1M" / "128K" / raw, picking
// the most readable form. Boundaries are exact multiples to avoid awkward
// numbers like "1.0M" when a matcher returns 1_000_000.
func formatContextWindow(tokens int) string {
	switch {
	case tokens >= 1_000_000 && tokens%1_000_000 == 0:
		return fmt.Sprintf("%dM", tokens/1_000_000)
	case tokens >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(tokens)/1_000_000)
	case tokens >= 1024 && tokens%1024 == 0:
		return fmt.Sprintf("%dK", tokens/1024)
	case tokens >= 1000:
		return fmt.Sprintf("%dK", tokens/1000)
	default:
		return fmt.Sprintf("%d", tokens)
	}
}

// tabModels returns the models visible under the current pricing tab and
// capability filter (ANDed). Chat-capable models come first, then the
// gateway's non-chat models (STT/TTS/image-gen/video) as view-only rows.
func (m *ModelSelectorImpl) tabModels() []string {
	pricing := m.pricingPredicate()
	capability := m.capabilityPredicate()
	all := append(append([]string{}, m.models...), models.NonChatModels()...)

	filtered := make([]string, 0, len(all))
	for _, model := range all {
		if pricing(model) && capability(model) {
			filtered = append(filtered, model)
		}
	}
	return filtered
}

func (m *ModelSelectorImpl) pricingPredicate() func(string) bool {
	switch m.currentView {
	case ModelViewFree:
		return m.isModelFree
	case ModelViewPayAsYouGo:
		return func(model string) bool {
			return !m.isModelFree(model) && !m.isModelSubscription(model)
		}
	case ModelViewSubscription:
		return m.isModelSubscription
	default:
		return func(string) bool { return true }
	}
}

func (m *ModelSelectorImpl) capabilityPredicate() func(string) bool {
	switch m.capability {
	case CapabilityVision:
		return models.SupportsVision
	case CapabilityAudio:
		return models.SupportsAudio
	case CapabilityVideo:
		return models.SupportsVideo
	default:
		return func(string) bool { return true }
	}
}

// isModelFree checks if a model is free (both input and output prices are 0.0).
// Subscription models are also $0/$0 but are not free, so they are excluded.
// Returns false if pricing is disabled or not configured.
func (m *ModelSelectorImpl) isModelFree(model string) bool {
	if m.pricingService == nil || !m.pricingService.IsEnabled() {
		return false
	}

	if m.isModelSubscription(model) {
		return false
	}

	inputPrice := m.pricingService.GetInputPrice(model)
	outputPrice := m.pricingService.GetOutputPrice(model)

	return inputPrice == 0.0 && outputPrice == 0.0
}

// isModelSubscription reports whether a model is accessed via a flat-fee
// subscription rather than per-token billing. It follows the pricing table's
// RequiresPro flag.
func (m *ModelSelectorImpl) isModelSubscription(model string) bool {
	if m.pricingService == nil || !m.pricingService.IsEnabled() {
		return false
	}

	return m.pricingService.RequiresPro(model)
}

// Reset clears the done/cancelled flags and rebuilds the form so the selector
// can be re-entered after a previous selection.
func (m *ModelSelectorImpl) Reset() {
	m.done = false
	m.cancelled = false
	m.searchMode = false
	m.search.Blur()
	m.search.SetValue("")
	m.buildForm()
}

// IsSelected returns true if a model was selected
func (m *ModelSelectorImpl) IsSelected() bool {
	return m.done && !m.cancelled
}

// IsCancelled returns true if selection was cancelled
func (m *ModelSelectorImpl) IsCancelled() bool {
	return m.cancelled
}

// GetSelected returns the selected model
func (m *ModelSelectorImpl) GetSelected() string {
	if m.IsSelected() {
		return m.choice
	}
	return ""
}

// SetWidth sets the width of the model selector
func (m *ModelSelectorImpl) SetWidth(width int) {
	m.width = width
}

// SetHeight sets the height of the model selector
func (m *ModelSelectorImpl) SetHeight(height int) {
	m.height = height
}

// writeViewTabs writes the pricing tab row (keys 1-4) and the capability tab
// row (keys 5-8), active tab highlighted.
func (m *ModelSelectorImpl) writeViewTabs(b *strings.Builder) {
	pricingTabs := []string{"[1] All", "[2] Free", "[3] Pay-as-you-go", "[4] Subscription"}
	capabilityTabs := []string{"[5] Any", "[6] Vision", "[7] Audio", "[8] Video"}

	b.WriteString(m.renderTabRow(pricingTabs, int(m.currentView)))
	b.WriteString("\n")
	b.WriteString(m.renderTabRow(capabilityTabs, int(m.capability)))
	b.WriteString("\n")

	separator := m.styleProvider.RenderDimText(strings.Repeat("─", max(m.width, 1)))
	fmt.Fprintf(b, "%s\n\n", separator)
}

// renderTabRow renders one row of tab labels with the active index
// highlighted.
func (m *ModelSelectorImpl) renderTabRow(labels []string, active int) string {
	rendered := make([]string, len(labels))
	for i, label := range labels {
		if i == active {
			rendered[i] = m.tabActiveStyle.Render(label)
		} else {
			rendered[i] = m.tabInactiveStyle.Render(label)
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
}
