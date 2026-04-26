package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	yaml "gopkg.in/yaml.v3"
)

const (
	ChannelsFileName    = "channels.yaml"
	DefaultChannelsPath = ConfigDirName + "/" + ChannelsFileName
)

// DefaultChannelsConfig returns the in-code default channels configuration
// used when no channels.yaml file exists. `infer init` seeds the file from
// this and the runtime falls back to it when the file is absent.
func DefaultChannelsConfig() *ChannelsConfig {
	return &ChannelsConfig{
		Enabled:         false,
		MaxWorkers:      5,
		ImageRetention:  5,
		RequireApproval: true,
		Telegram: TelegramChannelConfig{
			Enabled:      false,
			BotToken:     "",
			AllowedUsers: []string{},
			PollTimeout:  30,
		},
		WhatsApp: WhatsAppChannelConfig{
			Enabled:       false,
			PhoneNumberID: "",
			AccessToken:   "",
			VerifyToken:   "",
			WebhookPort:   8443,
			AllowedUsers:  []string{},
		},
	}
}

// LoadChannels reads channels.yaml from disk. When the file is missing it
// returns the in-code defaults so callers can treat absence as "use
// defaults" without special-casing. The file body is run through
// os.ExpandEnv so `${BOT_TOKEN}`-style references resolve from the
// environment.
func LoadChannels(path string) (*ChannelsConfig, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return DefaultChannelsConfig(), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read channels config: %w", err)
	}

	expandedData := os.ExpandEnv(string(data))

	var cfg ChannelsConfig
	if err := yaml.Unmarshal([]byte(expandedData), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse channels config: %w", err)
	}

	return &cfg, nil
}

// SaveChannels writes the channels configuration to disk, creating any
// missing parent directories. The file holds bot tokens / access tokens,
// so callers should ensure it is also listed in
// tools.sandbox.protected_paths.
func SaveChannels(path string, cfg *ChannelsConfig) error {
	var buf bytes.Buffer
	buf.WriteString("---\n")

	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)

	if err := encoder.Encode(cfg); err != nil {
		return fmt.Errorf("failed to marshal channels config: %w", err)
	}

	if err := encoder.Close(); err != nil {
		return fmt.Errorf("failed to close encoder: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write channels config: %w", err)
	}

	return nil
}
