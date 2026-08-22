package root

import (
	"slices"
	"testing"

	require "github.com/stretchr/testify/require"

	output "github.com/inference-gateway/cli/cmd/output"
)

func TestCommandTopology(t *testing.T) {
	command := NewCommand()
	want := []string{
		"agents", "chat", "config", "conversation-title", "conversations", "daemon", "debug", "env",
		"export", "gpu", "headless", "init", "keybindings", "mcp", "migrate", "plans", "plugins",
		"skills", "stats", "status", "tools", "traces", "version",
	}
	got := make([]string, 0, len(command.Commands()))
	for _, child := range command.Commands() {
		got = append(got, child.Name())
	}
	slices.Sort(got)
	slices.Sort(want)
	require.Equal(t, want, got)

	chatCommand, _, err := command.Find([]string{"chat"})
	require.NoError(t, err)
	require.Equal(t, "true", chatCommand.Annotations[output.TUICommandAnnotation])
	headlessCommand, _, err := command.Find([]string{"headless"})
	require.NoError(t, err)
	require.Empty(t, headlessCommand.Annotations[output.TUICommandAnnotation])

	for _, path := range [][]string{{"agents", "add"}, {"config", "set"}, {"gpu", "provision"}, {"mcp", "list"}} {
		found, _, err := command.Find(path)
		require.NoError(t, err)
		require.Equal(t, path[len(path)-1], found.Name())
	}
}

func TestNewCommandReturnsIndependentTrees(t *testing.T) {
	first := NewCommand()
	second := NewCommand()
	require.NotSame(t, first, second)
	firstChat, _, err := first.Find([]string{"chat"})
	require.NoError(t, err)
	secondChat, _, err := second.Find([]string{"chat"})
	require.NoError(t, err)
	require.NotSame(t, firstChat, secondChat)
}
