package config_test

import (
	"testing"
	"time"

	config "github.com/inference-gateway/cli/config"
)

func TestMemoryConfig_Validate(t *testing.T) {
	git := func(g config.MemoryGitConfig) config.MemoryConfig {
		return config.MemoryConfig{
			Enabled:  true,
			MaxChars: 100,
			Backend:  config.MemoryBackendConfig{Type: config.MemoryBackendGit, Git: g},
		}
	}

	tests := []struct {
		name    string
		m       config.MemoryConfig
		wantErr bool
	}{
		{"default is valid", *config.DefaultMemoryConfig(), false},
		{"disabled ignores max_chars", config.MemoryConfig{Enabled: false, MaxChars: 0}, false},
		{"enabled requires positive max_chars", config.MemoryConfig{Enabled: true, MaxChars: 0}, true},
		{"git requires repo when enabled", git(config.MemoryGitConfig{}), true},
		{"git with repo is valid", git(config.MemoryGitConfig{Repo: "git@github.com:o/r.git"}), false},
		{"unknown backend type", config.MemoryConfig{Enabled: true, MaxChars: 100, Backend: config.MemoryBackendConfig{Type: "s3"}}, true},
		{"unknown on_start", git(config.MemoryGitConfig{Repo: "r", Sync: config.MemoryGitSyncConfig{OnStart: "fetch"}}), true},
		{"unknown on_finish", git(config.MemoryGitConfig{Repo: "r", Sync: config.MemoryGitSyncConfig{OnFinish: "commit"}}), true},
		{"off sync values are valid", git(config.MemoryGitConfig{Repo: "r", Sync: config.MemoryGitSyncConfig{OnStart: config.MemorySyncOff, OnFinish: config.MemorySyncOff}}), false},
		{"git without repo but disabled is valid", config.MemoryConfig{Enabled: false, Backend: config.MemoryBackendConfig{Type: config.MemoryBackendGit}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.m.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMemoryGitConfig_Effective(t *testing.T) {
	empty := config.MemoryGitConfig{}
	if got := empty.EffectiveBranch(); got != config.DefaultMemoryGitBranch {
		t.Errorf("EffectiveBranch() = %q, want %q", got, config.DefaultMemoryGitBranch)
	}
	if got := empty.EffectiveCommitMessage(); got != config.DefaultMemoryGitCommitMessage {
		t.Errorf("EffectiveCommitMessage() = %q, want %q", got, config.DefaultMemoryGitCommitMessage)
	}
	if got := empty.EffectiveTimeout(); got != config.DefaultMemoryGitTimeout*time.Second {
		t.Errorf("EffectiveTimeout() = %v, want %v", got, config.DefaultMemoryGitTimeout*time.Second)
	}

	set := config.MemoryGitConfig{Branch: "dev", CommitMessage: "sync!", Timeout: 5}
	if got := set.EffectiveBranch(); got != "dev" {
		t.Errorf("EffectiveBranch() = %q, want dev", got)
	}
	if got := set.EffectiveCommitMessage(); got != "sync!" {
		t.Errorf("EffectiveCommitMessage() = %q, want sync!", got)
	}
	if got := set.EffectiveTimeout(); got != 5*time.Second {
		t.Errorf("EffectiveTimeout() = %v, want 5s", got)
	}

	for value, want := range map[string]bool{"": true, config.MemorySyncPull: true, config.MemorySyncOff: false} {
		if got := (config.MemoryGitConfig{Sync: config.MemoryGitSyncConfig{OnStart: value}}).PullOnStart(); got != want {
			t.Errorf("PullOnStart(on_start=%q) = %v, want %v", value, got, want)
		}
	}
	for value, want := range map[string]bool{"": true, config.MemorySyncPush: true, config.MemorySyncOff: false} {
		if got := (config.MemoryGitConfig{Sync: config.MemoryGitSyncConfig{OnFinish: value}}).PushOnFinish(); got != want {
			t.Errorf("PushOnFinish(on_finish=%q) = %v, want %v", value, got, want)
		}
	}
}
