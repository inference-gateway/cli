package utils

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/inference-gateway/cli/config"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// WriteViperConfigWithIndent writes the current Viper config with specified indentation
// This uses the same approach as config init to ensure consistent YAML structure
func WriteViperConfigWithIndent(v *viper.Viper, indent int) error {
	filename := v.ConfigFileUsed()
	if filename == "" {
		return fmt.Errorf("no config file is currently being used")
	}

	cfg := config.DefaultConfig()

	if err := v.Unmarshal(cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	var buf bytes.Buffer
	yamlEncoder := yaml.NewEncoder(&buf)
	yamlEncoder.SetIndent(indent)

	if err := yamlEncoder.Encode(cfg); err != nil {
		return fmt.Errorf("failed to marshal config to YAML: %w", err)
	}

	if err := yamlEncoder.Close(); err != nil {
		return fmt.Errorf("failed to close YAML encoder: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(filename, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// WriteViperConfigSparse writes only the keys actually present in v (its
// AllSettings), without seeding from DefaultConfig. This is what makes a project
// ./.infer/config.yaml a true sparse override: it carries only the keys it sets,
// so initConfig's key-by-key merge leaves every unset key inherited from the
// userspace ~/.infer/ baseline. (WriteViperConfigWithIndent, by contrast, writes
// the full default-seeded config and is used for the home baseline file.)
func WriteViperConfigSparse(v *viper.Viper, indent int) error {
	filename := v.ConfigFileUsed()
	if filename == "" {
		return fmt.Errorf("no config file is currently being used")
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")
	yamlEncoder := yaml.NewEncoder(&buf)
	yamlEncoder.SetIndent(indent)

	if err := yamlEncoder.Encode(v.AllSettings()); err != nil {
		return fmt.Errorf("failed to marshal config to YAML: %w", err)
	}
	if err := yamlEncoder.Close(); err != nil {
		return fmt.Errorf("failed to close YAML encoder: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(filename, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
