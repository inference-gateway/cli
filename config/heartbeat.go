package config

import (
	configutils "github.com/inference-gateway/cli/config/utils"
)

const (
	HeartbeatFileName    = "heartbeat.yaml"
	DefaultHeartbeatPath = ConfigDirName + "/" + HeartbeatFileName
)

// HeartbeatConfig configures the periodic tick that spawns `infer headless` so
// the agent can check for pending work without user input. It is disabled by
// default. Its system prompt lives in prompts.yaml under
// agent.system_prompt_heartbeat, apart from these runtime knobs.
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

// SaveHeartbeat writes the heartbeat configuration to disk, creating
// any missing parent directories.
func SaveHeartbeat(path string, cfg *HeartbeatConfig) error {
	return configutils.SaveYAML(path, "heartbeat", cfg)
}
