package daemon

import (
	"os"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestFillBindingTokenSharesOneOwnerOnlyToken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	first, second := config.DefaultConfig(), config.DefaultConfig()
	first.Daemon.Binding.Enabled, second.Daemon.Binding.Enabled = true, true

	if err := FillBindingToken(first); err != nil {
		t.Fatalf("FillBindingToken: %v", err)
	}
	if err := FillBindingToken(second); err != nil {
		t.Fatalf("FillBindingToken: %v", err)
	}

	if first.Daemon.Binding.Token == "" || first.Daemon.Binding.Token != second.Daemon.Binding.Token {
		t.Fatalf("tokens %q and %q, want one shared token", first.Daemon.Binding.Token, second.Daemon.Binding.Token)
	}
	info, err := os.Stat(LocalTokenFile())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("token file %v, %v, want mode 0600", info, err)
	}
}

func TestFillBindingTokenLeavesOtherBindingsAlone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	off := config.DefaultConfig()
	configured := config.DefaultConfig()
	configured.Daemon.Binding = config.BindingConfig{Enabled: true, Token: "mine"}

	for name, cfg := range map[string]*config.Config{"off": off, "configured": configured} {
		before := cfg.Daemon.Binding.Token
		if err := FillBindingToken(cfg); err != nil || cfg.Daemon.Binding.Token != before {
			t.Errorf("%s: token %q, %v, want %q kept", name, cfg.Daemon.Binding.Token, err, before)
		}
	}
	if _, err := os.Stat(LocalTokenFile()); !os.IsNotExist(err) {
		t.Errorf("a token file was created for bindings that need none: %v", err)
	}
}
