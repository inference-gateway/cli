package chat

import (
	"slices"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestWebSessionsOverSSHRunInDaemonModeWhenThisChatDoes(t *testing.T) {
	tests := []struct {
		name       string
		daemon     bool
		flagged    []string
		configured []string
	}{
		{name: "daemon chat", daemon: true, flagged: []string{"--daemon"}, configured: []string{"--model", "m1", "--daemon"}},
		{name: "in-process chat", configured: []string{"--model", "m1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flagged := config.DefaultConfig()
			flagged.Chat.Daemon = tt.daemon
			command := NewCommand(nil)
			if err := command.Flags().Set("ssh-host", "box.internal"); err != nil {
				t.Fatal(err)
			}
			applyWebFlags(command, flagged)
			if got := flagged.Web.Servers[0].CommandArgs; !slices.Equal(got, tt.flagged) {
				t.Errorf("the --ssh-host server runs chat %v, want %v", got, tt.flagged)
			}

			configured := config.DefaultConfig()
			configured.Chat.Daemon = tt.daemon
			configured.Web.Servers = []config.SSHServerConfig{{ID: "box", CommandArgs: []string{"--model", "m1"}}}
			applyWebFlags(NewCommand(nil), configured)
			if got := configured.Web.Servers[0].CommandArgs; !slices.Equal(got, tt.configured) {
				t.Errorf("a configured server runs chat %v, want %v", got, tt.configured)
			}
		})
	}
}
