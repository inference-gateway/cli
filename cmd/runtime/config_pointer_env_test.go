package runtime

import (
	"strings"
	"testing"

	viper "github.com/spf13/viper"

	config "github.com/inference-gateway/cli/config"
)

// newEnvViper mirrors State.Initialize: defaults registered from
// config.DefaultConfig(), INFER prefix, AutomaticEnv, "." -> "_" replacer.
func newEnvViper() *viper.Viper {
	v := viper.New()
	registerConfigDefaults(v, config.DefaultConfig())
	v.SetEnvPrefix("INFER")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	return v
}

// Pointer options were skipped entirely by the resolver, so every documented
// INFER_*_REQUIRE_APPROVAL silently did nothing.
func TestPointerOptionsHonourEnvOverrides(t *testing.T) {
	tests := []struct {
		name string
		env  string
		set  string
		read func(*config.Config) *bool
		want bool
	}{
		{
			"text to speech opt in",
			"INFER_TEXT_TO_SPEECH_REQUIRE_APPROVAL", "true",
			func(c *config.Config) *bool { return c.TextToSpeech.RequireApproval },
			true,
		},
		{
			"agent opt out",
			"INFER_TOOLS_AGENT_REQUIRE_APPROVAL", "false",
			func(c *config.Config) *bool { return c.Tools.Agent.RequireApproval },
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.env, tt.set)

			cfg := config.DefaultConfig()
			resolveViperEnvironmentVariables(newEnvViper(), cfg, "")

			got := tt.read(cfg)
			if got == nil {
				t.Fatalf("%s left the option unset", tt.env)
			}
			if *got != tt.want {
				t.Errorf("%s = %v, want %v", tt.env, *got, tt.want)
			}
		})
	}
}

// TestPointerDefaultsSurviveWithoutEnvOverride verifies registered pointer
// defaults remain value-preserving when Viper reloads them.
func TestPointerDefaultsSurviveWithoutEnvOverride(t *testing.T) {
	cfg := config.DefaultConfig()

	before := map[string]*bool{
		"write":  cfg.Tools.Write.RequireApproval,
		"edit":   cfg.Tools.Edit.RequireApproval,
		"delete": cfg.Tools.Delete.RequireApproval,
		"agent":  cfg.Tools.Agent.RequireApproval,
	}
	for name, ptr := range before {
		if ptr == nil {
			t.Fatalf("precondition: %s should ship a non-nil default", name)
		}
	}
	want := map[string]bool{}
	for name, ptr := range before {
		want[name] = *ptr
	}

	resolveViperEnvironmentVariables(newEnvViper(), cfg, "")

	after := map[string]*bool{
		"write":  cfg.Tools.Write.RequireApproval,
		"edit":   cfg.Tools.Edit.RequireApproval,
		"delete": cfg.Tools.Delete.RequireApproval,
		"agent":  cfg.Tools.Agent.RequireApproval,
	}
	for name, ptr := range after {
		if ptr == nil {
			t.Errorf("%s.require_approval was cleared with no env override", name)
			continue
		}
		if *ptr != want[name] {
			t.Errorf("%s.require_approval = %v, want %v (no env override was set)", name, *ptr, want[name])
		}
	}
}

// An unset option must stay unset, so IsApprovalRequired keeps falling through
// to the global tools.safety.require_approval default.
func TestPointerOptionsStayNilWithoutEnv(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.TextToSpeech.RequireApproval != nil {
		t.Fatal("precondition: text_to_speech.require_approval ships unset")
	}

	resolveViperEnvironmentVariables(newEnvViper(), cfg, "")

	if cfg.TextToSpeech.RequireApproval != nil {
		t.Errorf("expected the option to stay unset, got %v", *cfg.TextToSpeech.RequireApproval)
	}
}

// TestPointerOptionPrecedence verifies the binary's defaults, config file, and
// environment precedence chain.
func TestPointerOptionPrecedence(t *testing.T) {
	const yaml = `
tools:
  write:
    require_approval: true
text_to_speech:
  enabled: true
  require_approval: false
`

	tests := []struct {
		name    string
		env     map[string]string
		wantTTS bool
		wantWr  bool
	}{
		{
			name:    "config file wins over defaults",
			wantTTS: false,
			wantWr:  true,
		},
		{
			name:    "env wins over config file",
			env:     map[string]string{"INFER_TEXT_TO_SPEECH_REQUIRE_APPROVAL": "true"},
			wantTTS: true,
			wantWr:  true,
		},
		{
			name:    "env can turn an option off",
			env:     map[string]string{"INFER_TOOLS_WRITE_REQUIRE_APPROVAL": "false"},
			wantTTS: false,
			wantWr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, val := range tt.env {
				t.Setenv(k, val)
			}

			v := newEnvViper()
			v.SetConfigType("yaml")
			if err := v.ReadConfig(strings.NewReader(yaml)); err != nil {
				t.Fatalf("reading config: %v", err)
			}

			cfg := &config.Config{}
			if err := v.Unmarshal(cfg); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			resolveViperEnvironmentVariables(v, cfg, "")

			if got := cfg.TextToSpeech.RequireApproval; got == nil || *got != tt.wantTTS {
				t.Errorf("text_to_speech.require_approval = %v, want %v", deref(got), tt.wantTTS)
			}
			if got := cfg.Tools.Write.RequireApproval; got == nil || *got != tt.wantWr {
				t.Errorf("tools.write.require_approval = %v, want %v", deref(got), tt.wantWr)
			}

			if got := cfg.IsApprovalRequired("TextToSpeech"); got != tt.wantTTS {
				t.Errorf("IsApprovalRequired(TextToSpeech) = %v, want %v", got, tt.wantTTS)
			}
		})
	}
}

func deref(b *bool) any {
	if b == nil {
		return "<unset>"
	}
	return *b
}
