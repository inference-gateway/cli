package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultJudgeConfig(t *testing.T) {
	cfg := DefaultJudgeConfig()
	if cfg.Model != "" {
		t.Errorf("default judge model = %q, want empty (falls back to agent.model)", cfg.Model)
	}
	if cfg.Timeout != DefaultJudgeTimeoutSeconds {
		t.Errorf("default judge timeout = %d, want %d", cfg.Timeout, DefaultJudgeTimeoutSeconds)
	}
	if cfg.MaxTokens != DefaultJudgeMaxTokens {
		t.Errorf("default judge max_tokens = %d, want %d", cfg.MaxTokens, DefaultJudgeMaxTokens)
	}
	if cfg.OnError != JudgeOnErrorDeny {
		t.Errorf("default judge on_error = %q, want %q", cfg.OnError, JudgeOnErrorDeny)
	}
	for _, placeholder := range []string{"{root_intent}", "{intent}", "{action}"} {
		if !strings.Contains(cfg.Prompt, placeholder) {
			t.Errorf("default judge prompt missing %s placeholder", placeholder)
		}
	}
}

func TestLoadJudge(t *testing.T) {
	t.Run("missing file returns defaults", func(t *testing.T) {
		cfg, err := LoadJudge(filepath.Join(t.TempDir(), "absent.yaml"))
		if err != nil {
			t.Fatalf("LoadJudge() error = %v, want nil", err)
		}
		if *cfg != *DefaultJudgeConfig() {
			t.Errorf("LoadJudge(missing) = %+v, want defaults %+v", *cfg, *DefaultJudgeConfig())
		}
	})

	t.Run("reads the file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "judge.yaml")
		body := "model: openai/gpt-5\ntimeout: 5\nmax_tokens: 64\non_error: allow\n"
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("writing judge.yaml: %v", err)
		}
		cfg, err := LoadJudge(path)
		if err != nil {
			t.Fatalf("LoadJudge() error = %v, want nil", err)
		}
		if cfg.Model != "openai/gpt-5" || cfg.Timeout != 5 || cfg.MaxTokens != 64 || cfg.OnError != JudgeOnErrorAllow {
			t.Errorf("LoadJudge() = %+v, want parsed values", cfg)
		}
	})

	t.Run("invalid yaml errors", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "judge.yaml")
		if err := os.WriteFile(path, []byte("model: [unclosed"), 0o600); err != nil {
			t.Fatalf("writing judge.yaml: %v", err)
		}
		if _, err := LoadJudge(path); err == nil {
			t.Error("LoadJudge(invalid yaml) should return an error")
		}
	})

	t.Run("partial file leaves zeros until Effective applies defaults", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "judge.yaml")
		if err := os.WriteFile(path, []byte("model: openai/gpt-5\n"), 0o600); err != nil {
			t.Fatalf("writing judge.yaml: %v", err)
		}
		cfg, err := LoadJudge(path)
		if err != nil {
			t.Fatalf("LoadJudge() error = %v, want nil", err)
		}
		eff := cfg.Effective()
		if eff.Model != "openai/gpt-5" {
			t.Errorf("effective model = %q, want openai/gpt-5", eff.Model)
		}
		if eff.Timeout != DefaultJudgeTimeoutSeconds || eff.MaxTokens != DefaultJudgeMaxTokens {
			t.Errorf("effective timeout/max_tokens = %d/%d, want defaults", eff.Timeout, eff.MaxTokens)
		}
		if eff.OnError != JudgeOnErrorDeny || eff.Prompt != DefaultJudgePrompt || eff.SystemPrompt != DefaultJudgeSystemPrompt {
			t.Errorf("effective on_error/prompt not defaulted: %+v", eff)
		}
	})
}

func TestJudgeConfig_Effective(t *testing.T) {
	zero := JudgeConfig{}.Effective()
	if zero.Timeout != DefaultJudgeTimeoutSeconds || zero.MaxTokens != DefaultJudgeMaxTokens ||
		zero.OnError != JudgeOnErrorDeny || zero.Prompt != DefaultJudgePrompt {
		t.Errorf("Effective() of zero value = %+v, want all defaults applied", zero)
	}

	explicit := JudgeConfig{Timeout: 3, MaxTokens: 16, OnError: JudgeOnErrorAllow, Prompt: "custom"}.Effective()
	if explicit.Timeout != 3 || explicit.MaxTokens != 16 || explicit.OnError != JudgeOnErrorAllow || explicit.Prompt != "custom" {
		t.Errorf("Effective() = %+v, want explicit values preserved", explicit)
	}

	negative := JudgeConfig{Timeout: -5, MaxTokens: -1}.Effective()
	if negative.Timeout != DefaultJudgeTimeoutSeconds || negative.MaxTokens != DefaultJudgeMaxTokens {
		t.Errorf("Effective() of negative values = %+v, want defaults", negative)
	}
}

func TestJudgeConfig_ResolveModel(t *testing.T) {
	tests := []struct {
		name       string
		judgeModel string
		agentModel string
		want       string
	}{
		{"judge model wins", "openai/gpt-5", "anthropic/claude", "openai/gpt-5"},
		{"falls back to agent model", "", "anthropic/claude", "anthropic/claude"},
		{"both empty", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (JudgeConfig{Model: tt.judgeModel}).ResolveModel(tt.agentModel); got != tt.want {
				t.Errorf("ResolveModel(%q) with judge model %q = %q, want %q", tt.agentModel, tt.judgeModel, got, tt.want)
			}
		})
	}
}

func TestJudgeConfig_Validate(t *testing.T) {
	for _, onErr := range []string{"", JudgeOnErrorDeny, JudgeOnErrorAllow} {
		cfg := DefaultJudgeConfig()
		cfg.OnError = onErr
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() with on_error %q returned error: %v", onErr, err)
		}
	}

	cfg := DefaultJudgeConfig()
	cfg.OnError = "bogus"
	if err := cfg.Validate(); err == nil {
		t.Error(`Validate() with on_error "bogus" should return an error`)
	}
}

func TestSaveJudgeRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "judge.yaml")
	want := DefaultJudgeConfig()
	want.Model = "openai/gpt-4o-mini"

	if err := SaveJudge(path, want); err != nil {
		t.Fatalf("SaveJudge: %v", err)
	}
	got, err := LoadJudge(path)
	if err != nil {
		t.Fatalf("LoadJudge: %v", err)
	}
	if *got != *want {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", got, want)
	}
}
