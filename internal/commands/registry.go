package commands

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Registry manages all available commands
type Registry struct {
	commands map[string]Command
	mutex    sync.RWMutex
}

// NewRegistry creates a new command registry
func NewRegistry() *Registry {
	return &Registry{
		commands: make(map[string]Command),
	}
}

// Register adds a command to the registry
func (r *Registry) Register(cmd Command) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.commands[cmd.GetName()] = cmd
}

// Unregister removes a command from the registry
func (r *Registry) Unregister(name string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	delete(r.commands, name)
}

// Get retrieves a command by name
func (r *Registry) Get(name string) (Command, bool) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	cmd, exists := r.commands[name]
	return cmd, exists
}

// List returns all registered command names
func (r *Registry) List() []string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	names := make([]string, 0, len(r.commands))
	for name := range r.commands {
		names = append(names, name)
	}

	sort.Strings(names)
	return names
}

// GetAll returns all registered commands
func (r *Registry) GetAll() []Command {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	commands := make([]Command, 0, len(r.commands))
	for _, cmd := range r.commands {
		commands = append(commands, cmd)
	}

	sort.Slice(commands, func(i, j int) bool {
		return commands[i].GetName() < commands[j].GetName()
	})

	return commands
}

// Execute executes a command by name with arguments
func (r *Registry) Execute(ctx context.Context, name string, args []string) (CommandResult, error) {
	cmd, exists := r.Get(name)
	if !exists {
		return CommandResult{}, fmt.Errorf("unknown command: %s", name)
	}

	if !cmd.CanExecute(args) {
		return CommandResult{
			Output:  fmt.Sprintf("Invalid arguments for command '%s'. Usage: %s", name, cmd.GetUsage()),
			Success: false,
		}, nil
	}

	return cmd.Execute(ctx, args)
}

// ParseCommand parses a command line input into command name and arguments
func (r *Registry) ParseCommand(input string) (string, []string, error) {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "/") {
		return "", nil, fmt.Errorf("commands must start with '/'")
	}

	// Remove the leading slash
	input = input[1:]

	parts := strings.Fields(input)
	if len(parts) == 0 {
		return "", nil, fmt.Errorf("empty command")
	}

	command := parts[0]
	args := parts[1:]

	return command, args, nil
}

// GetCommands returns a map of command names to descriptions
func (r *Registry) GetCommands() map[string]string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	commands := make(map[string]string)
	for name, cmd := range r.commands {
		commands[name] = cmd.GetDescription()
	}

	return commands
}

// GetCommandsStartingWith returns commands that start with the given prefix
func (r *Registry) GetCommandsStartingWith(prefix string) []string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	var matches []string
	for name := range r.commands {
		if strings.HasPrefix(name, prefix) {
			matches = append(matches, name)
		}
	}

	sort.Strings(matches)
	return matches
}
