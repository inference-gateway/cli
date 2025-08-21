package handlers

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/internal/services"
)

// MessageHandler interface for the modern architecture
type MessageHandler interface {
	CanHandle(msg tea.Msg) bool
	Handle(msg tea.Msg, stateManager *services.StateManager) (tea.Model, tea.Cmd)
	GetPriority() int
	GetName() string
}
