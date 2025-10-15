package handlers

import (
	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// EventHandler defines the interface for handling events
type EventHandler interface {
	Handle(msg tea.Msg, stateManager domain.StateManager) (tea.Model, tea.Cmd)
}

// Compile-time assertion that ChatHandler implements EventHandler
var _ EventHandler = (*ChatHandler)(nil)
