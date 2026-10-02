package factory

import (
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
	shortcuts "github.com/inference-gateway/cli/internal/presentation/shortcuts"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	autocomplete "github.com/inference-gateway/cli/internal/presentation/tui/autocomplete"
	components "github.com/inference-gateway/cli/internal/presentation/tui/components"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
)

// CreateConversationView creates a new conversation view component
func CreateConversationView(themeService tui.ThemeService) tui.ConversationRenderer {
	styleProvider := styles.NewProvider(themeService)
	return components.NewConversationView(styleProvider)
}

// CreateInputView creates a new input view component
func CreateInputView(modelService convdomain.ModelService) tui.InputComponent {
	return components.NewInputView(modelService)
}

// CreateInputViewWithName creates a new input view component with a history base
// directory and name. When store is non-nil and name is empty (the main agent),
// input history goes through the storage backend. Named subagent histories are
// file-based at <baseDir>/history/history-<name>.
func CreateInputViewWithName(modelService convdomain.ModelService, baseDir, name string, store storage.ShellHistoryStorage) tui.InputComponent {
	return components.NewInputViewWithName(modelService, baseDir, name, store)
}

// CreateAutocomplete creates a new autocomplete component
func CreateAutocomplete(shortcutRegistry *shortcuts.Registry, toolService agentdomain.ToolService, modelService convdomain.ModelService, pricingService convdomain.PricingService, skillsService agentdomain.SkillsService, githubIssueService agentdomain.GitHubIssueService) tui.AutocompleteComponent {
	if shortcutRegistry == nil {
		return nil
	}

	ac := autocomplete.NewAutocomplete(tui.NewDefaultTheme(), shortcutRegistry)
	if toolService != nil {
		ac.SetToolService(toolService)
	}
	if modelService != nil {
		ac.SetModelService(modelService)
	}
	if pricingService != nil {
		ac.SetPricingService(pricingService)
	}
	if skillsService != nil {
		ac.SetSkillsService(skillsService)
	}
	if githubIssueService != nil {
		ac.SetGitHubIssueService(githubIssueService)
	}
	return ac
}

// CreateStatusView creates a new status view component
func CreateStatusView(themeService tui.ThemeService) tui.StatusComponent {
	styleProvider := styles.NewProvider(themeService)
	return components.NewStatusView(styleProvider)
}

// CreateInputStatusBar creates a new input status bar component
func CreateInputStatusBar(themeService tui.ThemeService) tui.InputStatusBarComponent {
	styleProvider := styles.NewProvider(themeService)
	return components.NewInputStatusBar(styleProvider)
}

// CreateHelpBar creates a new help bar component
func CreateHelpBar(themeService tui.ThemeService) tui.HelpBarComponent {
	styleProvider := styles.NewProvider(themeService)
	return components.NewHelpBar(styleProvider)
}
