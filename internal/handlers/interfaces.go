package handlers

import (
	"github.com/charmbracelet/bubbletea"
)

// MessageHandler interface for handling different types of messages
type MessageHandler interface {
	CanHandle(msg tea.Msg) bool
	Handle(msg tea.Msg, state *AppState) (tea.Model, tea.Cmd)
	GetPriority() int
}

// AppState represents the application state that handlers can access and modify
type AppState struct {
	// Current view state
	CurrentView ViewType

	// Services (read-only references)
	ConversationRepo interface{}
	ModelService     interface{}
	ChatService      interface{}
	ToolService      interface{}
	FileService      interface{}

	// UI state
	Width  int
	Height int
	Error  string
	Status string

	// Additional state data
	Data map[string]interface{}
}

// ViewType represents different application views
type ViewType int

const (
	ViewModelSelection ViewType = iota
	ViewChat
	ViewFileSelection
	ViewCommandSelection
	ViewApproval
	ViewHelp
)
