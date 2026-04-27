package config

import (
	utils "github.com/inference-gateway/cli/config/utils"
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
	Enabled      bool     `yaml:"enabled" mapstructure:"enabled"`
	BotToken     string   `yaml:"bot_token" mapstructure:"bot_token"`
	AllowedUsers []string `yaml:"allowed_users" mapstructure:"allowed_users"`
	PollTimeout  int      `yaml:"poll_timeout" mapstructure:"poll_timeout"`
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
	return utils.LoadYAML(path, "channels", DefaultChannelsConfig)
}

// SaveChannels writes the channels configuration to disk, creating any
// missing parent directories. The file holds bot tokens / access tokens,
// so callers should ensure it is also listed in
// tools.sandbox.protected_paths.
func SaveChannels(path string, cfg *ChannelsConfig) error {
	return utils.SaveYAML(path, "channels", cfg)
}
