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
func CreateInputView(modelService domain.ModelService, shortcutRegistry *shortcuts.Registry) ui.InputComponent {
	iv := components.NewInputView(modelService)

	if shortcutRegistry != nil {
		iv.Autocomplete = autocomplete.NewAutocomplete(ui.NewDefaultTheme(), shortcutRegistry)
	}
	return iv
}

// CreateInputViewWithToolService creates a new input view component with tool service
func CreateInputViewWithToolService(modelService domain.ModelService, shortcutRegistry *shortcuts.Registry, toolService domain.ToolService) ui.InputComponent {
	return CreateInputViewWithToolServiceAndConfigDir(modelService, shortcutRegistry, toolService, "")
}

// CreateInputViewWithToolServiceAndConfigDir creates a new input view component with tool service and config directory
func CreateInputViewWithToolServiceAndConfigDir(modelService domain.ModelService, shortcutRegistry *shortcuts.Registry, toolService domain.ToolService, configDir string) ui.InputComponent {
	iv := components.NewInputViewWithConfigDir(modelService, configDir)

	if shortcutRegistry != nil {
		autocomplete := autocomplete.NewAutocomplete(ui.NewDefaultTheme(), shortcutRegistry)
		if toolService != nil {
			autocomplete.SetToolService(toolService)
		}
		iv.Autocomplete = autocomplete
	}
	return iv
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
