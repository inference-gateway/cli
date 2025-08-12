package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbletea"
)

// CommandSelectorImpl implements command selection UI
type CommandSelectorImpl struct {
	commands         []CommandOption
	filteredCommands []CommandOption
	selected         int
	width            int
	height           int
	theme            Theme
	done             bool
	cancelled        bool
	searchQuery      string
	searchMode       bool
}

// CommandOption represents a command with its description
type CommandOption struct {
	Command     string
	Description string
}

// GetAvailableCommands returns the list of available chat commands
func GetAvailableCommands() []CommandOption {
	return []CommandOption{
		{"/exit", "Exit chat session"},
		{"/quit", "Exit chat session"},
		{"/clear", "Clear conversation history"},
		{"/history", "Show conversation history"},
		{"/models", "Show current and available models"},
		{"/switch", "Switch to a different model"},
		{"/compact", "Export conversation to markdown file"},
		{"/help", "Show help information"},
	}
}

// NewCommandSelector creates a new command selector
func NewCommandSelector(theme Theme) *CommandSelectorImpl {
	commands := GetAvailableCommands()
	c := &CommandSelectorImpl{
		commands:         commands,
		filteredCommands: make([]CommandOption, len(commands)),
		selected:         0,
		width:            80,
		height:           24,
		theme:            theme,
		searchQuery:      "",
		searchMode:       false,
	}
	copy(c.filteredCommands, commands)
	return c
}

func (c *CommandSelectorImpl) Init() tea.Cmd {
	return nil
}

func (c *CommandSelectorImpl) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		c.width = msg.Width
		c.height = msg.Height
		return c, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			c.cancelled = true
			c.done = true
			return c, tea.Quit

		case "up", "k":
			if c.selected > 0 {
				c.selected--
			}
			return c, nil

		case "down", "j":
			if c.selected < len(c.filteredCommands)-1 {
				c.selected++
			}
			return c, nil

		case "enter", " ":
			if len(c.filteredCommands) > 0 {
				selectedCommand := c.filteredCommands[c.selected].Command
				c.done = true
				return c, func() tea.Msg {
					return CommandSelectedMsg{Command: selectedCommand}
				}
			}
			return c, nil

		case "esc":
			if c.searchMode {
				c.searchMode = false
				c.searchQuery = ""
				c.filterCommands()
				c.selected = 0
				return c, nil
			}
			c.cancelled = true
			c.done = true
			return c, tea.Quit

		case "/":
			c.searchMode = true
			return c, nil

		case "backspace":
			if c.searchMode && len(c.searchQuery) > 0 {
				c.searchQuery = c.searchQuery[:len(c.searchQuery)-1]
				c.filterCommands()
				c.selected = 0
			}
			return c, nil

		default:
			if c.searchMode && len(msg.String()) == 1 && msg.String()[0] >= 32 {
				c.searchQuery += msg.String()
				c.filterCommands()
				c.selected = 0
			}
			return c, nil
		}
	}

	return c, nil
}

func (c *CommandSelectorImpl) View() string {
	var b strings.Builder

	// Header
	b.WriteString(fmt.Sprintf("%s🔧 Select a Command%s\n\n",
		c.theme.GetAccentColor(), "\033[0m"))

	// Search box
	if c.searchMode {
		b.WriteString(fmt.Sprintf("%sSearch: %s%s│%s\n\n",
			c.theme.GetStatusColor(), c.searchQuery, c.theme.GetAccentColor(), "\033[0m"))
	} else {
		b.WriteString(fmt.Sprintf("%sPress / to search • %d commands available%s\n\n",
			c.theme.GetDimColor(), len(c.commands), "\033[0m"))
	}

	if len(c.filteredCommands) == 0 {
		if c.searchQuery != "" {
			b.WriteString(fmt.Sprintf("%sNo commands match '%s'%s\n",
				c.theme.GetErrorColor(), c.searchQuery, "\033[0m"))
		} else {
			b.WriteString(fmt.Sprintf("%sNo commands available%s\n",
				c.theme.GetErrorColor(), "\033[0m"))
		}
		return b.String()
	}

	// Command list
	maxVisible := c.height - 10 // Reserve space for header, search, and footer
	if maxVisible > len(c.filteredCommands) {
		maxVisible = len(c.filteredCommands)
	}

	start := 0
	if c.selected >= maxVisible {
		start = c.selected - maxVisible + 1
	}

	for i := start; i < start+maxVisible && i < len(c.filteredCommands); i++ {
		cmd := c.filteredCommands[i]

		if i == c.selected {
			b.WriteString(fmt.Sprintf("%s▶ %-12s%s %s%s%s\n",
				c.theme.GetAccentColor(), cmd.Command, "\033[0m",
				c.theme.GetDimColor(), cmd.Description, "\033[0m"))
		} else {
			b.WriteString(fmt.Sprintf("  %-12s %s%s%s\n",
				cmd.Command, c.theme.GetDimColor(), cmd.Description, "\033[0m"))
		}
	}

	if len(c.filteredCommands) > maxVisible {
		b.WriteString(fmt.Sprintf("\n%sShowing %d-%d of %d commands%s\n",
			c.theme.GetDimColor(), start+1, start+maxVisible, len(c.filteredCommands), "\033[0m"))
	}

	// Footer
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", c.width))
	b.WriteString("\n")
	if c.searchMode {
		b.WriteString(fmt.Sprintf("%sType to search, ↑↓ to navigate, Enter to select, Esc to clear search%s",
			c.theme.GetDimColor(), "\033[0m"))
	} else {
		b.WriteString(fmt.Sprintf("%sUse ↑↓ arrows to navigate, Enter to select, / to search, Esc/Ctrl+C to cancel%s",
			c.theme.GetDimColor(), "\033[0m"))
	}

	return b.String()
}

// filterCommands filters the commands based on the search query
func (c *CommandSelectorImpl) filterCommands() {
	if c.searchQuery == "" {
		c.filteredCommands = make([]CommandOption, len(c.commands))
		copy(c.filteredCommands, c.commands)
		return
	}

	c.filteredCommands = c.filteredCommands[:0]
	query := strings.ToLower(c.searchQuery)

	for _, cmd := range c.commands {
		if strings.Contains(strings.ToLower(cmd.Command), query) ||
			strings.Contains(strings.ToLower(cmd.Description), query) {
			c.filteredCommands = append(c.filteredCommands, cmd)
		}
	}
}

// IsSelected returns true if a command was selected
func (c *CommandSelectorImpl) IsSelected() bool {
	return c.done && !c.cancelled
}

// IsCancelled returns true if selection was cancelled
func (c *CommandSelectorImpl) IsCancelled() bool {
	return c.cancelled
}

// GetSelected returns the selected command
func (c *CommandSelectorImpl) GetSelected() string {
	if c.IsSelected() && len(c.filteredCommands) > 0 {
		return c.filteredCommands[c.selected].Command
	}
	return ""
}

// SetWidth sets the width of the command selector
func (c *CommandSelectorImpl) SetWidth(width int) {
	c.width = width
}

// SetHeight sets the height of the command selector
func (c *CommandSelectorImpl) SetHeight(height int) {
	c.height = height
}
