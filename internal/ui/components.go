package ui

import (
	"github.com/inference-gateway/cli/internal/commands"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/ui/components"
)

// CreateConversationView creates a new conversation view component
func CreateConversationView() ConversationRenderer {
	return components.NewConversationView()
}

// CreateInputView creates a new input view component
func CreateInputView(modelService domain.ModelService, commandRegistry *commands.Registry) InputComponent {
	iv := components.NewInputView(modelService)

	if commandRegistry != nil {
		iv.Autocomplete = NewAutocomplete(NewDefaultTheme(), commandRegistry)
	}
	return iv
}

// CreateInputViewWithToolService creates a new input view component with tool service
func CreateInputViewWithToolService(modelService domain.ModelService, commandRegistry *commands.Registry, toolService domain.ToolService) InputComponent {
	iv := components.NewInputView(modelService)

	if commandRegistry != nil {
		autocomplete := NewAutocomplete(NewDefaultTheme(), commandRegistry)
		if toolService != nil {
			autocomplete.SetToolService(toolService)
		}
		iv.Autocomplete = autocomplete
	}
	return iv
}

// CreateStatusView creates a new status view component
func CreateStatusView() StatusComponent {
	return components.NewStatusView()
}

// CreateHelpBar creates a new help bar component
func CreateHelpBar() HelpBarComponent {
	return components.NewHelpBar()
}

// CreateApprovalView creates a new approval view component
func CreateApprovalView(theme Theme) ApprovalComponent {
	return components.NewApprovalComponent(theme)
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
