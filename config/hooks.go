package config

import (
	"cmp"
	"fmt"
	"time"

	utils "github.com/inference-gateway/cli/config/utils"
	domain "github.com/inference-gateway/cli/internal/domain"
)

const (
	HooksFileName    = "hooks.yaml"
	DefaultHooksPath = ConfigDirName + "/" + HooksFileName
)

const defaultHookTimeoutSeconds = 30

const defaultGofmtHookCommand = "gofmt -w ."

// HookCommandConfig is one named command hook: a shell command run at a
// pre-defined agent-loop hook point with a wall-clock timeout. It is the
// command-action counterpart of ReminderConfig (the text-injection action).
type HookCommandConfig struct {
	Name    string           `yaml:"name" mapstructure:"name"`
	Hook    domain.HookPoint `yaml:"hook" mapstructure:"hook"`
	Command string           `yaml:"command" mapstructure:"command"`
	Timeout int              `yaml:"timeout,omitempty" mapstructure:"timeout"` // seconds; 0 -> default
}

// HooksConfig is the content of hooks.yaml: the master switch plus the list of
// command hooks. Each hook attaches a shell command to a pre-defined hook point
// (domain.HookPoint). HooksConfig implements domain.HookCommandProvider. It is
// the executable sibling of RemindersConfig (reminders.yaml): "run code" and
// "inject text" stay separate concerns in separate files. Off by default;
// commands still face the bash allow-list when the agent runs them.
type HooksConfig struct {
	Enabled bool                `yaml:"enabled" mapstructure:"enabled"`
	Hooks   []HookCommandConfig `yaml:"hooks" mapstructure:"hooks"`
}

// DefaultHooksConfig returns the in-code default used when no hooks.yaml exists.
// Hooks ship disabled with one illustrative post_session command so the shape is
// discoverable; flipping enabled=true (and allow-listing the command) is enough.
func DefaultHooksConfig() *HooksConfig {
	return &HooksConfig{
		Enabled: false,
		Hooks: []HookCommandConfig{
			{
				Name:    "gofmt",
				Hook:    domain.HookPostSession,
				Command: defaultGofmtHookCommand,
				Timeout: defaultHookTimeoutSeconds,
			},
		},
	}
}

// LoadHooks reads hooks.yaml from disk. When the file is missing it returns the
// in-code defaults so callers can treat absence as "use defaults".
func LoadHooks(path string) (*HooksConfig, error) {
	return utils.LoadYAML(path, "hooks", DefaultHooksConfig)
}

// SaveHooks writes the hooks configuration to disk, creating any missing parent
// directories.
func SaveHooks(path string, cfg *HooksConfig) error {
	return utils.SaveYAML(path, "hooks", cfg)
}

// effective returns the hooks with per-entry defaults applied: a non-positive
// timeout falls back to defaultHookTimeoutSeconds. The hook point is left as
// configured (an explicit hook is required, enforced by Validate) so there is no
// surprising default firing point.
func (h HooksConfig) effective() []HookCommandConfig {
	out := make([]HookCommandConfig, len(h.Hooks))
	for i, hc := range h.Hooks {
		hc.Timeout = cmp.Or(hc.Timeout, defaultHookTimeoutSeconds)
		out[i] = hc
	}
	return out
}

// CommandsDue implements domain.HookCommandProvider: it returns every command
// hook attached to the given hook point. Multiple commands on the same point
// stack (all are returned, in config order). Command hooks need no trigger - the
// hook point's own cadence is the cadence (e.g. post_session fires once per run).
// The agent gates each returned command on the bash allow-list before running it.
func (h HooksConfig) CommandsDue(hook domain.HookPoint) []domain.HookCommand {
	if !h.Enabled {
		return nil
	}
	var due []domain.HookCommand
	for _, hc := range h.effective() {
		if hc.Hook != hook {
			continue
		}
		due = append(due, domain.HookCommand{
			Name:    hc.Name,
			Command: hc.Command,
			Timeout: time.Duration(hc.Timeout) * time.Second,
		})
	}
	return due
}

// Validate checks each command hook for a name, a command, and an explicit valid
// hook point, plus a non-negative timeout. It returns an error describing the
// first invalid entry.
func (h HooksConfig) Validate() error {
	for i, hc := range h.Hooks {
		switch {
		case hc.Name == "":
			return fmt.Errorf("hooks[%d]: name is required", i)
		case hc.Command == "":
			return fmt.Errorf("hooks[%d] (%s): command is required", i, hc.Name)
		case hc.Hook == "":
			return fmt.Errorf("hooks[%d] (%s): hook is required (valid: %v)", i, hc.Name, domain.HookPoints)
		case !hc.Hook.Valid():
			return fmt.Errorf("hooks[%d] (%s): unknown hook %q (valid: %v)", i, hc.Name, hc.Hook, domain.HookPoints)
		case hc.Timeout < 0:
			return fmt.Errorf("hooks[%d] (%s): timeout must be >= 0", i, hc.Name)
		}
	}
	return nil
}
