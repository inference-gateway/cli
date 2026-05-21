package config

import (
	utils "github.com/inference-gateway/cli/config/utils"
)

const (
	HeartbeatFileName    = "heartbeat.yaml"
	DefaultHeartbeatPath = ConfigDirName + "/" + HeartbeatFileName
)

// HeartbeatConfig configures the periodic "wake-up" tick that spawns
// `infer agent` with a tailored system prompt so the agent can check
// for pending work without waiting for user input. Disabled by default.
//
// The companion system prompt lives at
// PromptsConfig.Agent.SystemPromptHeartbeat in prompts.yaml - keeping
// runtime knobs (interval, model) here separate from prompt text.
type HeartbeatConfig struct {
	Enabled      bool   `yaml:"enabled" mapstructure:"enabled"`
	Interval     string `yaml:"interval" mapstructure:"interval"`
	InitialDelay string `yaml:"initial_delay" mapstructure:"initial_delay"`
	Model        string `yaml:"model" mapstructure:"model"`
	Prompt       string `yaml:"prompt" mapstructure:"prompt"`
}

// DefaultHeartbeatConfig returns the in-code default heartbeat
// configuration used when no heartbeat.yaml file exists. `infer init`
// seeds the file from this and the runtime falls back to it when the
// file is absent.
func DefaultHeartbeatConfig() *HeartbeatConfig {
	return &HeartbeatConfig{
		Enabled:      false,
		Interval:     "1h",
		InitialDelay: "1m",
		Model:        "",
		Prompt:       "Heartbeat tick - check for any pending tasks, todos, or background work and act on them.",
	}
}

// LoadHeartbeat reads heartbeat.yaml from disk. When the file is
// missing it returns the in-code defaults so callers can treat absence
// as "use defaults" without special-casing. The file body is run
// through os.ExpandEnv so `${VAR}`-style references resolve from the
// environment.
func LoadHeartbeat(path string) (*HeartbeatConfig, error) {
	return utils.LoadYAML(path, "heartbeat", DefaultHeartbeatConfig)
}

// SaveHeartbeat writes the heartbeat configuration to disk, creating
// any missing parent directories.
func SaveHeartbeat(path string, cfg *HeartbeatConfig) error {
	return utils.SaveYAML(path, "heartbeat", cfg)
}
