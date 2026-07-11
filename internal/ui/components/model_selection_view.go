package components

import (
	"fmt"
	"strings"

	textinput "charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	huh "charm.land/huh/v2"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	models "github.com/inference-gateway/cli/internal/models"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// ModelViewMode defines the different filter modes for models
type ModelViewMode int

const (
	ModelViewAll ModelViewMode = iota
	ModelViewFree
	ModelViewPayAsYouGo
	ModelViewSubscription
)

// modelSelectChromeLines is the vertical space around the huh select: title,
// tabs, separator, blank lines, and the help row.
const modelSelectChromeLines = 8

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
	modelService   domain.ModelService
	pricingService domain.PricingService
	config         *config.Config
	currentView    ModelViewMode

	form       *huh.Form
	sel        *huh.Select[string]
	choice     string
	search     textinput.Model
	searchMode bool
}

// NewModelSelector creates a new model selector
func NewModelSelector(models []string, modelService domain.ModelService, pricingService domain.PricingService, cfg *config.Config, styleProvider *styles.Provider) *ModelSelectorImpl {
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
		if msg.String() == "ctrl+c" {
			m.cancelled = true
			m.done = true
			return m, tea.Quit
		}
		if m.searchMode {
			return m, m.handleSearchKey(msg)
		}
		switch msg.String() {
		case "1", "2", "3", "4":
			m.handleViewSwitch(msg.String())
			return m, nil
		case "/":
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
	switch msg.String() {
	case "esc":
		m.searchMode = false
		m.search.Blur()
		if m.search.Value() != "" {
			m.search.SetValue("")
			m.buildForm()
		}
		return nil
	case "enter", "up", "down":
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
		return nil
	}
	m.done = true
	return func() tea.Msg {
		return domain.ModelSelectedEvent{Model: selectedModel}
	}
}

func (m *ModelSelectorImpl) handleViewSwitch(key string) {
	switch key {
	case "1":
		m.currentView = ModelViewAll
	case "2":
		m.currentView = ModelViewFree
	case "3":
		m.currentView = ModelViewPayAsYouGo
	case "4":
		m.currentView = ModelViewSubscription
	}
	m.buildForm()
}

func (m *ModelSelectorImpl) View() tea.View {
	return tea.NewView(m.viewContent())
}

func (m *ModelSelectorImpl) viewContent() string {
	var b strings.Builder

	accentColor := m.styleProvider.GetThemeColor("accent")
	b.WriteString(m.styleProvider.RenderWithColor("Select a Model", accentColor))
	b.WriteString("\n\n")

	m.writeViewTabs(&b)

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
	b.WriteString(strings.Repeat("─", max(m.width, 1)))
	b.WriteString("\n")
	b.WriteString(m.styleProvider.RenderDimText("Use ↑↓ arrows to navigate, Enter to select, / to search, esc to clear, 1-4 to switch tabs, Ctrl+C to cancel"))

	return b.String()
}

// formatModelSuffix builds the parenthesised metadata shown next to each
// model row, combining the context window (compact "128K"/"1M" form, or "?"
// when no matcher pattern hits) with the pricing string when available.
func (m *ModelSelectorImpl) formatModelSuffix(model string) string {
	parts := make([]string, 0, 2)

	window, ok := models.LookupContextWindow(model)
	if ok {
		parts = append(parts, formatContextWindow(window))
	} else {
		parts = append(parts, "?")
	}

	if label := domain.FormatModelPricingLabel(m.pricingService, model); label != "" {
		parts = append(parts, label)
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

// tabModels returns the models visible under the current pricing tab.
func (m *ModelSelectorImpl) tabModels() []string {
	switch m.currentView {
	case ModelViewFree:
		return m.filterModels(m.isModelFree)
	case ModelViewPayAsYouGo:
		return m.filterModels(func(model string) bool {
			return !m.isModelFree(model) && !m.isModelSubscription(model)
		})
	case ModelViewSubscription:
		return m.filterModels(m.isModelSubscription)
	default:
		return m.models
	}
}

func (m *ModelSelectorImpl) filterModels(keep func(string) bool) []string {
	filtered := make([]string, 0, len(m.models))
	for _, model := range m.models {
		if keep(model) {
			filtered = append(filtered, model)
		}
	}
	return filtered
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

// writeViewTabs writes the view selection tabs
func (m *ModelSelectorImpl) writeViewTabs(b *strings.Builder) {
	accentColor := m.styleProvider.GetThemeColor("accent")

	allStyle := "[1] All"
	freeStyle := "[2] Free"
	paygStyle := "[3] Pay-as-you-go"
	subscriptionStyle := "[4] Subscription"

	switch m.currentView {
	case ModelViewAll:
		allStyle = m.styleProvider.RenderWithColor("[1] All", accentColor)
	case ModelViewFree:
		freeStyle = m.styleProvider.RenderWithColor("[2] Free", accentColor)
	case ModelViewPayAsYouGo:
		paygStyle = m.styleProvider.RenderWithColor("[3] Pay-as-you-go", accentColor)
	case ModelViewSubscription:
		subscriptionStyle = m.styleProvider.RenderWithColor("[4] Subscription", accentColor)
	}

	tabs := fmt.Sprintf("%s  %s  %s  %s", allStyle, freeStyle, paygStyle, subscriptionStyle)
	dimTabs := m.styleProvider.RenderDimText(tabs)
	fmt.Fprintf(b, "%s\n", dimTabs)

	separatorWidth := m.width - 4
	if separatorWidth < 0 {
		separatorWidth = 40
	}
	separator := m.styleProvider.RenderDimText(strings.Repeat("─", separatorWidth))
	fmt.Fprintf(b, "%s\n\n", separator)
}
