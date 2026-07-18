package cmd

import (
	"testing"

	config "github.com/inference-gateway/cli/config"
)

// TestBuildChannelShortcutRegistryMetadataSafe guards the nil-dependency
// registry: the channel manager only reads metadata from built-in shortcuts,
// so every getter must be safe to call without wired services.
func TestBuildChannelShortcutRegistryMetadataSafe(t *testing.T) {
	reg := buildChannelShortcutRegistry(config.DefaultConfig())

	all := reg.GetAll()
	if len(all) == 0 {
		t.Fatal("expected registered shortcuts")
	}
	for _, sc := range all {
		if sc.GetName() == "" {
			t.Fatal("shortcut with empty name")
		}
		_ = sc.GetDescription()
		_ = sc.GetUsage()
	}

	cmds := supportedChannelCommands(reg)
	for _, want := range []string{"new", "conversations", "stats", "help"} {
		found := false
		for _, c := range cmds {
			if c.Name == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected %q in supported channel commands, got %v", want, cmds)
		}
	}
}
