package factory

import (
	domain "github.com/inference-gateway/cli/internal/domain"
	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
	ui "github.com/inference-gateway/cli/internal/ui"
	autocomplete "github.com/inference-gateway/cli/internal/ui/autocomplete"
	components "github.com/inference-gateway/cli/internal/ui/components"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// CreateConversationView creates a new conversation view component
func CreateConversationView(themeService domain.ThemeService) ui.ConversationRenderer {
	styleProvider := styles.NewProvider(themeService)
	return components.NewConversationView(styleProvider)
}

// CreateInputView creates a new input view component
func CreateInputView(modelService domain.ModelService) ui.InputComponent {
	return components.NewInputView(modelService)
}

// CreateInputViewWithConfigDir creates a new input view component with config directory
func CreateInputViewWithConfigDir(modelService domain.ModelService, configDir string) ui.InputComponent {
	return components.NewInputViewWithConfigDir(modelService, configDir)
}

// CreateAutocomplete creates a new autocomplete component
func CreateAutocomplete(shortcutRegistry *shortcuts.Registry, toolService domain.ToolService, modelService domain.ModelService, pricingService domain.PricingService) ui.AutocompleteComponent {
	if shortcutRegistry == nil {
		return nil
	}

	ac := autocomplete.NewAutocomplete(ui.NewDefaultTheme(), shortcutRegistry)
	if toolService != nil {
		ac.SetToolService(toolService)
	}
	if modelService != nil {
		ac.SetModelService(modelService)
	}
	if pricingService != nil {
		ac.SetPricingService(pricingService)
	}
	return ac
}

// CreateStatusView creates a new status view component
func CreateStatusView(themeService domain.ThemeService) ui.StatusComponent {
	styleProvider := styles.NewProvider(themeService)
	return components.NewStatusView(styleProvider)
}

// CreateInputStatusBar creates a new input status bar component
func CreateInputStatusBar(themeService domain.ThemeService) ui.InputStatusBarComponent {
	styleProvider := styles.NewProvider(themeService)
	return components.NewInputStatusBar(styleProvider)
}

// CreateHelpBar creates a new help bar component
func CreateHelpBar(themeService domain.ThemeService) ui.HelpBarComponent {
	styleProvider := styles.NewProvider(themeService)
	return components.NewHelpBar(styleProvider)
}

// Layout calculations - simplified without interfaces
func CalculateConversationHeight(totalHeight int) int {
	inputHeight := CalculateInputHeight(totalHeight)
	statusHeight := CalculateStatusHeight(totalHeight)

	extraLines := 5
	if totalHeight < 12 {
		extraLines = 3
	}

	conversationHeight := totalHeight - inputHeight - statusHeight - extraLines

	minConversationHeight := 3
	if conversationHeight < minConversationHeight {
		conversationHeight = minConversationHeight
	}

	return conversationHeight
}

func CalculateInputHeight(totalHeight int) int {
	if totalHeight < 8 {
		return 2
	}
	if totalHeight < 12 {
		return 3
	}
	return 4
}

func CalculateStatusHeight(totalHeight int) int {
	if totalHeight < 8 {
		return 0
	}
	if totalHeight < 12 {
		return 1
	}
	return 2
}

func GetMargins() (top, right, bottom, left int) {
	return 1, 2, 1, 2
}
