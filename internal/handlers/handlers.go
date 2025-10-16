package handlers

import (
	tea "github.com/charmbracelet/bubbletea"
)

// EventHandler defines the interface for handling events
type EventHandler interface {
	Handle(msg tea.Msg) tea.Cmd
}

// Compile-time assertion that ChatHandler implements EventHandler
var _ EventHandler = (*ChatHandler)(nil)
