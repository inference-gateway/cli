package tui

// ThemeService handles theme management
type ThemeService interface {
	ListThemes() []string
	GetCurrentTheme() Theme
	GetCurrentThemeName() string
	SetTheme(themeName string) error
}

// StatusType represents different types of status messages
type StatusType int

const (
	StatusDefault StatusType = iota
	StatusThinking
	StatusGenerating
	StatusWorking
	StatusProcessing
	StatusPreparing
	StatusError
)

// StatusProgress represents progress information for status messages
type StatusProgress struct {
	Current int
	Total   int
}
