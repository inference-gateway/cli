package config

import (
	utils "github.com/inference-gateway/cli/config/utils"
)

const (
	MemoryConfigFileName    = "memory.yaml"
	DefaultMemoryConfigPath = ConfigDirName + "/" + MemoryConfigFileName
)

// MemoryConfig configures the agent's persistent, cross-session memory. When
// enabled, durable facts are stored as individual Markdown fact-files under a
// global directory (~/.infer/memory by default), catalogued by an index file
// (MEMORY.md) that the Memory tool keeps in sync. The index is injected into
// context at session start; the agent reads individual facts on demand.
//
// Runtime knobs live here (in memory.yaml); the tool's LLM-facing description
// lives at PromptsConfig.Tools.Memory.Description in prompts.yaml, and the
// session reminder lives in reminders.yaml - keeping each concern in its own
// file, like Heartbeat/ComputerUse/Channels.
type MemoryConfig struct {
	Enabled  bool   `yaml:"enabled" mapstructure:"enabled"`
	Dir      string `yaml:"dir" mapstructure:"dir"`             // "" => ~/.infer/memory
	MaxChars int    `yaml:"max_chars" mapstructure:"max_chars"` // cap on the injected MEMORY.md index
}

// DefaultMemoryConfig returns the in-code default memory configuration used when
// no memory.yaml file exists. `infer init` seeds the file from this and the
// runtime falls back to it when the file is absent. Memory ships disabled by
// default; flipping enabled=true is enough to turn it on.
func DefaultMemoryConfig() *MemoryConfig {
	return &MemoryConfig{
		Enabled:  false,
		Dir:      "",
		MaxChars: DefaultMemoryMaxChars,
	}
}

// LoadMemory reads memory.yaml from disk. When the file is missing it returns
// the in-code defaults so callers can treat absence as "use defaults".
func LoadMemory(path string) (*MemoryConfig, error) {
	return utils.LoadYAML(path, "memory", DefaultMemoryConfig)
}

// SaveMemory writes the memory configuration to disk, creating any missing
// parent directories.
func SaveMemory(path string, cfg *MemoryConfig) error {
	return utils.SaveYAML(path, "memory", cfg)
}
