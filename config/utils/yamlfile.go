// Package utils provides generic file-IO helpers shared by every sub-config
// in package config. They are deliberately domain-agnostic - anything that
// knows about a specific config type belongs in package config itself.
package utils

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	yaml "gopkg.in/yaml.v3"
)

// LoadYAML reads path. If the file does not exist, defaults() is returned so
// callers can treat absence as "use defaults" without special-casing. The
// file body is run through os.ExpandEnv so ${VAR} references resolve from
// the environment before unmarshalling - any future content that needs a
// literal `${…}` token must escape it as `$$…`.
//
// label scopes error messages, e.g. "channels" produces
// "failed to read channels config: …".
func LoadYAML[T any](path, label string, defaults func() *T) (*T, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return defaults(), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s config: %w", label, err)
	}

	expanded := os.ExpandEnv(string(data))

	cfg := new(T)
	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, fmt.Errorf("failed to parse %s config: %w", label, err)
	}

	return cfg, nil
}

// ParseYAML unmarshals raw YAML bytes into cfg without environment-variable
// expansion. It is the low-level parse used when the caller has already read
// the file and wants to control expansion (e.g. plugin hooks.yaml must not
// expand env vars from plugin-controlled content).
func ParseYAML[T any](data []byte, label string, cfg *T) error {
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("failed to parse %s config: %w", label, err)
	}
	return nil
}

// SaveYAML writes cfg to path, creating any missing parent directories.
// It always emits the YAML document marker `---\n` and uses 2-space indent.
func SaveYAML[T any](path, label string, cfg *T) error {
	var buf bytes.Buffer
	buf.WriteString("---\n")

	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)

	if err := encoder.Encode(cfg); err != nil {
		return fmt.Errorf("failed to marshal %s config: %w", label, err)
	}

	if err := encoder.Close(); err != nil {
		return fmt.Errorf("failed to close %s encoder: %w", label, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create %s config directory: %w", label, err)
	}

	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write %s config: %w", label, err)
	}

	return nil
}
