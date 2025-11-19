package ui

import (
	"github.com/inference-gateway/cli/internal/ui/shared"
	"github.com/inference-gateway/cli/internal/ui/styles/colors"
)

// Type aliases to shared interfaces to avoid duplication
type KeyShortcut = shared.KeyShortcut
type Theme = shared.Theme
type ConversationRenderer = shared.ConversationRenderer
type InputComponent = shared.InputComponent
type StatusComponent = shared.StatusComponent
type HelpBarComponent = shared.HelpBarComponent
type ApprovalComponent = shared.ApprovalComponent

// DefaultTheme provides a concrete implementation of the Theme interface
type DefaultTheme struct{}

func NewDefaultTheme() *DefaultTheme { return &DefaultTheme{} }

func (t *DefaultTheme) GetUserColor() string       { return colors.UserColor.ANSI }
func (t *DefaultTheme) GetAssistantColor() string  { return colors.AssistantColor.ANSI }
func (t *DefaultTheme) GetErrorColor() string      { return colors.ErrorColor.ANSI }
func (t *DefaultTheme) GetSuccessColor() string    { return colors.SuccessColor.ANSI }
func (t *DefaultTheme) GetStatusColor() string     { return colors.StatusColor.ANSI }
func (t *DefaultTheme) GetAccentColor() string     { return colors.AccentColor.ANSI }
func (t *DefaultTheme) GetDimColor() string        { return colors.DimColor.ANSI }
func (t *DefaultTheme) GetBorderColor() string     { return colors.BorderColor.ANSI }
func (t *DefaultTheme) GetDiffAddColor() string    { return colors.DiffAddColor.ANSI }
func (t *DefaultTheme) GetDiffRemoveColor() string { return colors.DiffRemoveColor.ANSI }

// Compile-time check to ensure AutocompleteImpl implements the interface
var _ shared.AutocompleteInterface = (*AutocompleteImpl)(nil)

// SelectionComponent is specific to UI layer (not duplicated in shared)
type SelectionComponent interface {
	GetOptions() []string
	SetOptions(options []string)
	GetSelected() string
	GetSelectedIndex() int
	SetSelected(index int)
	IsSelected() bool
	IsCancelled() bool
	SetWidth(width int)
	SetHeight(height int)
	Render() string
}
