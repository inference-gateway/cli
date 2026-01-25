package components

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	domain "github.com/inference-gateway/cli/internal/domain"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// ModelViewMode defines the different filter modes for models
type ModelViewMode int

const (
	ModelViewAll ModelViewMode = iota
	ModelViewFree
	ModelViewProprietary
)

// ModelSelectorImpl implements model selection UI
type ModelSelectorImpl struct {
	models         []string
	filteredModels []string
	selected       int
	width          int
	height         int
	styleProvider  *styles.Provider
	done           bool
	cancelled      bool
	modelService   domain.ModelService
	pricingService domain.PricingService
	configService  domain.ConfigService
	searchQuery    string
	searchMode     bool
	currentView    ModelViewMode
}

// NewModelSelector creates a new model selector
func NewModelSelector(models []string, modelService domain.ModelService, pricingService domain.PricingService, configService domain.ConfigService, styleProvider *styles.Provider) *ModelSelectorImpl {
	m := &ModelSelectorImpl{
		models:         models,
		filteredModels: make([]string, len(models)),
		selected:       0,
		width:          80,
		height:         24,
		styleProvider:  styleProvider,
		modelService:   modelService,
		pricingService: pricingService,
		configService:  configService,
		searchQuery:    "",
		searchMode:     false,
		currentView:    ModelViewAll,
	}
	copy(m.filteredModels, models)
	return m
}

func (m *ModelSelectorImpl) Init() tea.Cmd {
	return nil
}

func (m *ModelSelectorImpl) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleWindowResize(msg)
	case tea.KeyMsg:
		return m.handleKeyInput(msg)
	}

	return m, nil
}

func (m *ModelSelectorImpl) handleWindowResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	return m, nil
}

func (m *ModelSelectorImpl) handleKeyInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m.handleCancel()
	case "up":
		return m.handleNavigationUp()
	case "down":
		return m.handleNavigationDown()
	case "enter", " ":
		return m.handleSelection()
	case "/":
		if !m.searchMode {
			return m.handleSearchToggle()
		}
		return m.handleCharacterInput(msg)
	case "backspace":
		return m.handleBackspace()
	case "1", "2", "3":
		m.handleViewSwitch(msg.String())
		return m, nil
	default:
		return m.handleCharacterInput(msg)
	}
}

func (m *ModelSelectorImpl) handleCancel() (tea.Model, tea.Cmd) {
	m.cancelled = true
	m.done = true
	return m, tea.Quit
}

func (m *ModelSelectorImpl) handleNavigationUp() (tea.Model, tea.Cmd) {
	if m.selected > 0 {
		m.selected--
	}
	return m, nil
}

func (m *ModelSelectorImpl) handleNavigationDown() (tea.Model, tea.Cmd) {
	if m.selected < len(m.filteredModels)-1 {
		m.selected++
	}
	return m, nil
}

func (m *ModelSelectorImpl) handleSelection() (tea.Model, tea.Cmd) {
	if len(m.filteredModels) > 0 {
		selectedModel := m.filteredModels[m.selected]
		if err := m.modelService.SelectModel(selectedModel); err == nil {
			m.done = true
			return m, func() tea.Msg {
				return domain.ModelSelectedEvent{Model: selectedModel}
			}
		}
	}
	return m, nil
}

func (m *ModelSelectorImpl) handleSearchToggle() (tea.Model, tea.Cmd) {
	m.searchMode = true
	return m, nil
}

func (m *ModelSelectorImpl) handleBackspace() (tea.Model, tea.Cmd) {
	if m.searchMode && len(m.searchQuery) > 0 {
		m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
		m.updateSearch()
	}
	return m, nil
}

func (m *ModelSelectorImpl) handleCharacterInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.searchMode && len(msg.String()) == 1 && msg.String()[0] >= 32 {
		m.searchQuery += msg.String()
		m.updateSearch()
	}
	return m, nil
}

func (m *ModelSelectorImpl) updateSearch() {
	m.applyFilters()
	m.selected = 0
}

func (m *ModelSelectorImpl) handleViewSwitch(key string) {
	switch key {
	case "1":
		m.currentView = ModelViewAll
	case "2":
		m.currentView = ModelViewFree
	case "3":
		m.currentView = ModelViewProprietary
	}
	m.selected = 0
	m.applyFilters()
}

func (m *ModelSelectorImpl) View() string {
	var b strings.Builder

	accentColor := m.styleProvider.GetThemeColor("accent")
	b.WriteString(m.styleProvider.RenderWithColor("Select a Model", accentColor))

	if m.configService != nil {
		cfg := m.configService.GetConfig()
		if cfg.ClaudeCode.Enabled {
			successColor := m.styleProvider.GetThemeColor("success")
			b.WriteString(" ")
			b.WriteString(m.styleProvider.RenderWithColor("● Claude Subscription", successColor))
		}
	}

	b.WriteString("\n\n")

	m.writeViewTabs(&b)

	if m.searchMode {
		statusColor := m.styleProvider.GetThemeColor("status")
		b.WriteString(m.styleProvider.RenderWithColor("Search: "+m.searchQuery, statusColor))
		b.WriteString(m.styleProvider.RenderWithColor("│", accentColor))
		b.WriteString("\n\n")
	} else {
		helpText := fmt.Sprintf("Press / to search • %d models available", len(m.filteredModels))
		b.WriteString(m.styleProvider.RenderDimText(helpText))
		b.WriteString("\n\n")
	}

	if len(m.filteredModels) == 0 {
		errorColor := m.styleProvider.GetThemeColor("error")

		if m.searchQuery != "" {
			b.WriteString(m.styleProvider.RenderWithColor(fmt.Sprintf("No models match '%s'", m.searchQuery), errorColor))
		} else {
			b.WriteString(m.styleProvider.RenderWithColor("No models available", errorColor))
		}
		b.WriteString("\n")
		return b.String()
	}

	maxVisible := m.height - 10
	if maxVisible > len(m.filteredModels) {
		maxVisible = len(m.filteredModels)
	}

	start := 0
	if m.selected >= maxVisible {
		start = m.selected - maxVisible + 1
	}

	for i := start; i < start+maxVisible && i < len(m.filteredModels); i++ {
		model := m.filteredModels[i]
		var pricingSuffix string
		if m.pricingService != nil {
			if pricing := m.pricingService.FormatModelPricing(model); pricing != "" {
				pricingSuffix = fmt.Sprintf("(%s)", pricing)
			}
		}

		if i == m.selected {
			b.WriteString(m.styleProvider.RenderWithColor("▶ "+model, accentColor))
			if pricingSuffix != "" {
				b.WriteString(" ")
				b.WriteString(m.styleProvider.RenderDimText(pricingSuffix))
			}
			b.WriteString("\n")
		} else {
			b.WriteString(fmt.Sprintf("  %s", model))
			if pricingSuffix != "" {
				b.WriteString(" ")
				b.WriteString(m.styleProvider.RenderDimText(pricingSuffix))
			}
			b.WriteString("\n")
		}
	}

	if len(m.filteredModels) > maxVisible {
		paginationText := fmt.Sprintf("Showing %d-%d of %d models", start+1, start+maxVisible, len(m.filteredModels))
		b.WriteString("\n")
		b.WriteString(m.styleProvider.RenderDimText(paginationText))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", m.width))
	b.WriteString("\n")

	if m.searchMode {
		b.WriteString(m.styleProvider.RenderDimText("Type to search, ↑↓ to navigate, Enter to select, Esc to clear search"))
	} else {
		b.WriteString(m.styleProvider.RenderDimText("Use ↑↓ arrows to navigate, Enter to select, / to search, 1-3 to filter, Esc/Ctrl+C to cancel"))
	}

	return b.String()
}

// applyFilters filters the models based on the current view and search query
func (m *ModelSelectorImpl) applyFilters() {
	var baseModels []string

	switch m.currentView {
	case ModelViewAll:
		baseModels = m.models
	case ModelViewFree:
		baseModels = make([]string, 0)
		for _, model := range m.models {
			if m.isModelFree(model) {
				baseModels = append(baseModels, model)
			}
		}
	case ModelViewProprietary:
		baseModels = make([]string, 0)
		for _, model := range m.models {
			if !m.isModelFree(model) {
				baseModels = append(baseModels, model)
			}
		}
	}

	if m.searchQuery == "" {
		m.filteredModels = baseModels
		return
	}

	m.filteredModels = make([]string, 0)
	query := strings.ToLower(m.searchQuery)

	for _, model := range baseModels {
		if strings.Contains(strings.ToLower(model), query) {
			m.filteredModels = append(m.filteredModels, model)
		}
	}
}

// isModelFree checks if a model is free (both input and output prices are 0.0)
// Returns false if pricing is disabled or not configured
func (m *ModelSelectorImpl) isModelFree(model string) bool {
	if m.pricingService == nil || !m.pricingService.IsEnabled() {
		return false
	}

	inputPrice := m.pricingService.GetInputPrice(model)
	outputPrice := m.pricingService.GetOutputPrice(model)

	return inputPrice == 0.0 && outputPrice == 0.0
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
	if m.IsSelected() && len(m.models) > 0 {
		return m.models[m.selected]
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
	proprietaryStyle := "[3] Proprietary"

	switch m.currentView {
	case ModelViewAll:
		allStyle = m.styleProvider.RenderWithColor("[1] All", accentColor)
	case ModelViewFree:
		freeStyle = m.styleProvider.RenderWithColor("[2] Free", accentColor)
	case ModelViewProprietary:
		proprietaryStyle = m.styleProvider.RenderWithColor("[3] Proprietary", accentColor)
	}

	tabs := fmt.Sprintf("%s  %s  %s", allStyle, freeStyle, proprietaryStyle)
	dimTabs := m.styleProvider.RenderDimText(tabs)
	fmt.Fprintf(b, "%s\n", dimTabs)

	separatorWidth := m.width - 4
	if separatorWidth < 0 {
		separatorWidth = 40
	}
	separator := m.styleProvider.RenderDimText(strings.Repeat("─", separatorWidth))
	fmt.Fprintf(b, "%s\n\n", separator)
}
