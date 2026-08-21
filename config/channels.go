package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	configutils "github.com/inference-gateway/cli/config/utils"
)

const (
	ChannelsFileName    = "channels.yaml"
	DefaultChannelsPath = ConfigDirName + "/" + ChannelsFileName
)

// ChannelsConfig contains configuration for external messaging channels
type ChannelsConfig struct {
	Enabled         bool                  `yaml:"enabled" mapstructure:"enabled"`
	MaxWorkers      int                   `yaml:"max_workers" mapstructure:"max_workers"`
	ImageRetention  int                   `yaml:"image_retention" mapstructure:"image_retention"`
	RequireApproval bool                  `yaml:"require_approval" mapstructure:"require_approval"`
	Telegram        TelegramChannelConfig `yaml:"telegram" mapstructure:"telegram"`
	WhatsApp        WhatsAppChannelConfig `yaml:"whatsapp" mapstructure:"whatsapp"`
}

// TelegramChannelConfig contains Telegram bot settings
type TelegramChannelConfig struct {
	Enabled      bool                `yaml:"enabled" mapstructure:"enabled"`
	BotToken     string              `yaml:"bot_token" mapstructure:"bot_token"`
	AllowedUsers []string            `yaml:"allowed_users" mapstructure:"allowed_users"`
	PollTimeout  int                 `yaml:"poll_timeout" mapstructure:"poll_timeout"`
	Media        TelegramMediaConfig `yaml:"media" mapstructure:"media"`
}

// TelegramMediaConfig controls saving inbound photo/video attachments to a
// local directory so the agent can use them as assets.
type TelegramMediaConfig struct {
	Enabled          bool     `yaml:"enabled" mapstructure:"enabled"`
	Dir              string   `yaml:"dir" mapstructure:"dir"`                               // "" -> ~/.infer/media
	MaxSizeMB        int      `yaml:"max_size_mb" mapstructure:"max_size_mb"`               // reject files larger than this (0 -> 10)
	Retain           int      `yaml:"retain" mapstructure:"retain"`                         // keep the last N files (0 -> 20)
	AllowedMimeTypes []string `yaml:"allowed_mime_types" mapstructure:"allowed_mime_types"` // empty -> built-in image/video defaults
}

// ResolveDir returns the directory where inbound media attachments are stored,
// defaulting to ~/.infer/media when Dir is unset.
func (c TelegramMediaConfig) ResolveDir() (string, error) {
	if strings.TrimSpace(c.Dir) != "" {
		return c.Dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ConfigDirName, "media"), nil
}

// WhatsAppChannelConfig contains WhatsApp Business API settings
type WhatsAppChannelConfig struct {
	Enabled       bool     `yaml:"enabled" mapstructure:"enabled"`
	PhoneNumberID string   `yaml:"phone_number_id" mapstructure:"phone_number_id"`
	AccessToken   string   `yaml:"access_token" mapstructure:"access_token"`
	VerifyToken   string   `yaml:"verify_token" mapstructure:"verify_token"`
	WebhookPort   int      `yaml:"webhook_port" mapstructure:"webhook_port"`
	AllowedUsers  []string `yaml:"allowed_users" mapstructure:"allowed_users"`
}

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
			Media: TelegramMediaConfig{
				Enabled:   false,
				Dir:       "",
				MaxSizeMB: 10,
				Retain:    20,
				AllowedMimeTypes: []string{
					"image/jpeg",
					"image/png",
					"image/webp",
					"video/mp4",
					"video/quicktime",
				},
			},
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
	return configutils.LoadYAML(path, "channels", DefaultChannelsConfig)
}

// SaveChannels writes the channels configuration to disk, creating any
// missing parent directories. The file holds bot tokens / access tokens,
// so callers should ensure it is also listed in
// tools.sandbox.protected_paths.
func SaveChannels(path string, cfg *ChannelsConfig) error {
	return configutils.SaveYAML(path, "channels", cfg)
}
