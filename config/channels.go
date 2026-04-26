package config

import (
	utils "github.com/inference-gateway/cli/config/utils"
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
	return utils.LoadYAML(path, "channels", DefaultChannelsConfig)
}

// SaveChannels writes the channels configuration to disk, creating any
// missing parent directories. The file holds bot tokens / access tokens,
// so callers should ensure it is also listed in
// tools.sandbox.protected_paths.
func SaveChannels(path string, cfg *ChannelsConfig) error {
	return utils.SaveYAML(path, "channels", cfg)
}
