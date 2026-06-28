package config_test

import (
	"path/filepath"
	"testing"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

func TestSaveLoadHooks_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.yaml")
	original := &config.HooksConfig{
		Enabled: true,
		Hooks: []config.HookCommandConfig{
			{Name: "gofmt", Hook: domain.HookPostSession, Command: "gofmt -w .", Timeout: 30},
		},
	}
	if err := config.SaveHooks(path, original); err != nil {
		t.Fatalf("SaveHooks: %v", err)
	}
	loaded, err := config.LoadHooks(path)
	if err != nil {
		t.Fatalf("LoadHooks: %v", err)
	}
	if !loaded.Enabled || len(loaded.Hooks) != 1 ||
		loaded.Hooks[0].Name != "gofmt" || loaded.Hooks[0].Hook != domain.HookPostSession ||
		loaded.Hooks[0].Command != "gofmt -w ." || loaded.Hooks[0].Timeout != 30 {
		t.Errorf("round-trip mismatch: %+v", loaded)
	}
}

func TestLoadHooks_MissingFileReturnsDefaults(t *testing.T) {
	loaded, err := config.LoadHooks(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("LoadHooks on missing file: %v", err)
	}
	if len(loaded.Hooks) == 0 {
		t.Error("missing file should yield the in-code defaults")
	}
	if loaded.Enabled {
		t.Error("hooks must ship disabled by default")
	}
}

func TestHooksFileConstants(t *testing.T) {
	if config.HooksFileName != "hooks.yaml" {
		t.Errorf("HooksFileName = %q, want hooks.yaml", config.HooksFileName)
	}
	if config.DefaultHooksPath != config.ConfigDirName+"/hooks.yaml" {
		t.Errorf("DefaultHooksPath = %q", config.DefaultHooksPath)
	}
}

func hooksCfg(enabled bool, hooks ...config.HookCommandConfig) config.HooksConfig {
	return config.HooksConfig{Enabled: enabled, Hooks: hooks}
}

func hookCmd(name string, hook domain.HookPoint, command string, timeout int) config.HookCommandConfig {
	return config.HookCommandConfig{Name: name, Hook: hook, Command: command, Timeout: timeout}
}

func TestCommandsDue(t *testing.T) {
	post := hookCmd("gofmt", domain.HookPostSession, "gofmt -w .", 30)
	pre := hookCmd("lint", domain.HookPreStream, "golangci-lint run", 0)

	tests := []struct {
		name     string
		cfg      config.HooksConfig
		hook     domain.HookPoint
		wantLen  int
		wantName string
	}{
		{"disabled returns nil", hooksCfg(false, post), domain.HookPostSession, 0, ""},
		{"matching hook returns command", hooksCfg(true, post), domain.HookPostSession, 1, "gofmt"},
		{"non-matching hook returns nil", hooksCfg(true, post), domain.HookPreStream, 0, ""},
		{"only commands on the queried hook", hooksCfg(true, post, pre), domain.HookPreStream, 1, "lint"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			due := tc.cfg.CommandsDue(tc.hook)
			if len(due) != tc.wantLen {
				t.Fatalf("CommandsDue(%s) len = %d, want %d (%+v)", tc.hook, len(due), tc.wantLen, due)
			}
			if tc.wantLen > 0 && due[0].Name != tc.wantName {
				t.Errorf("CommandsDue(%s)[0].Name = %q, want %q", tc.hook, due[0].Name, tc.wantName)
			}
		})
	}
}

func TestCommandsDue_TimeoutDefaulted(t *testing.T) {
	cfg := hooksCfg(true, hookCmd("lint", domain.HookPostSession, "golangci-lint run", 0))
	due := cfg.CommandsDue(domain.HookPostSession)
	if len(due) != 1 {
		t.Fatalf("expected 1 command, got %d", len(due))
	}
	if due[0].Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want default 30s", due[0].Timeout)
	}
}

func TestCommandsDue_Stacking(t *testing.T) {
	cfg := hooksCfg(true,
		hookCmd("gofmt", domain.HookPostSession, "gofmt -w .", 30),
		hookCmd("notify", domain.HookPostSession, "echo done", 5),
	)
	due := cfg.CommandsDue(domain.HookPostSession)
	if len(due) != 2 {
		t.Fatalf("both commands on the hook should be returned, got %d", len(due))
	}
}

func TestHooksConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.HooksConfig
		wantErr bool
	}{
		{"valid", hooksCfg(true, hookCmd("gofmt", domain.HookPostSession, "gofmt -w .", 30)), false},
		{"empty name", hooksCfg(true, hookCmd("", domain.HookPostSession, "gofmt -w .", 30)), true},
		{"empty command", hooksCfg(true, hookCmd("gofmt", domain.HookPostSession, "", 30)), true},
		{"empty hook", hooksCfg(true, hookCmd("gofmt", "", "gofmt -w .", 30)), true},
		{"unknown hook", hooksCfg(true, hookCmd("gofmt", domain.HookPoint("nope"), "gofmt -w .", 30)), true},
		{"negative timeout", hooksCfg(true, hookCmd("gofmt", domain.HookPostSession, "gofmt -w .", -1)), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestDefaultHooksConfig(t *testing.T) {
	cfg := config.DefaultHooksConfig()
	if cfg.Enabled {
		t.Error("default hooks must be disabled")
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("default hooks config must validate: %v", err)
	}
}
