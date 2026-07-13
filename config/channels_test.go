package config_test

import (
	"os"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestChannelsConstants(t *testing.T) {
	if config.ChannelsFileName != "channels.yaml" {
		t.Errorf("Expected ChannelsFileName 'channels.yaml', got %q", config.ChannelsFileName)
	}
	expectedPath := config.ConfigDirName + "/" + config.ChannelsFileName
	if config.DefaultChannelsPath != expectedPath {
		t.Errorf("Expected DefaultChannelsPath %q, got %q", expectedPath, config.DefaultChannelsPath)
	}
}

func TestDefaultChannelsConfig(t *testing.T) {
	cfg := config.DefaultChannelsConfig()
	if cfg == nil {
		t.Fatal("DefaultChannelsConfig() returned nil")
	}
	if cfg.Enabled {
		t.Error("Expected Enabled to be false by default")
	}
	if !cfg.RequireApproval {
		t.Error("Expected RequireApproval to be true by default")
	}
	if cfg.MaxWorkers != 5 {
		t.Errorf("Expected MaxWorkers=5, got %d", cfg.MaxWorkers)
	}
	if cfg.ImageRetention != 5 {
		t.Errorf("Expected ImageRetention=5, got %d", cfg.ImageRetention)
	}
	if cfg.Telegram.PollTimeout != 30 {
		t.Errorf("Expected Telegram.PollTimeout=30, got %d", cfg.Telegram.PollTimeout)
	}
	if cfg.Telegram.AllowedUsers == nil {
		t.Error("Expected Telegram.AllowedUsers to be non-nil empty slice")
	}
	if cfg.WhatsApp.WebhookPort != 8443 {
		t.Errorf("Expected WhatsApp.WebhookPort=8443, got %d", cfg.WhatsApp.WebhookPort)
	}
}

func checkChannelsValidYAML(t *testing.T, cfg *config.ChannelsConfig) {
	t.Helper()
	if !cfg.Enabled {
		t.Error("Expected Enabled true")
	}
	if cfg.MaxWorkers != 8 {
		t.Errorf("Expected MaxWorkers=8, got %d", cfg.MaxWorkers)
	}
	if cfg.RequireApproval {
		t.Error("Expected RequireApproval false")
	}
	if !cfg.Telegram.Enabled {
		t.Error("Expected Telegram.Enabled true")
	}
	if cfg.Telegram.BotToken != "abc123" {
		t.Errorf("Expected bot_token 'abc123', got %q", cfg.Telegram.BotToken)
	}
	if len(cfg.Telegram.AllowedUsers) != 2 {
		t.Errorf("Expected 2 allowed users, got %d", len(cfg.Telegram.AllowedUsers))
	}
	if cfg.Telegram.PollTimeout != 60 {
		t.Errorf("Expected poll_timeout=60, got %d", cfg.Telegram.PollTimeout)
	}
	if cfg.WhatsApp.WebhookPort != 9000 {
		t.Errorf("Expected webhook_port=9000, got %d", cfg.WhatsApp.WebhookPort)
	}
}

func TestLoadChannels(t *testing.T) {
	defaults := config.DefaultChannelsConfig()

	tests := []struct {
		name    string
		yaml    string
		env     map[string]string
		wantErr bool
		check   func(t *testing.T, cfg *config.ChannelsConfig)
	}{
		{
			name: "non-existent file returns defaults",
			check: func(t *testing.T, cfg *config.ChannelsConfig) {
				if cfg.Enabled != defaults.Enabled || cfg.MaxWorkers != defaults.MaxWorkers {
					t.Errorf("Expected defaults, got %+v", cfg)
				}
			},
		},
		{
			name: "valid yaml",
			yaml: `---
enabled: true
max_workers: 8
image_retention: 10
require_approval: false
telegram:
  enabled: true
  bot_token: "abc123"
  allowed_users:
    - "111"
    - "222"
  poll_timeout: 60
whatsapp:
  enabled: false
  phone_number_id: ""
  access_token: ""
  verify_token: ""
  webhook_port: 9000
  allowed_users: []
`,
			check: checkChannelsValidYAML,
		},
		{
			name: "environment variable expansion",
			env: map[string]string{
				"TEST_CHANNELS_BOT_TOKEN":    "expanded-token",
				"TEST_CHANNELS_ACCESS_TOKEN": "expanded-access",
			},
			yaml: `---
enabled: true
telegram:
  enabled: true
  bot_token: "${TEST_CHANNELS_BOT_TOKEN}"
whatsapp:
  access_token: "${TEST_CHANNELS_ACCESS_TOKEN}"
`,
			check: func(t *testing.T, cfg *config.ChannelsConfig) {
				if cfg.Telegram.BotToken != "expanded-token" {
					t.Errorf("Expected expanded bot_token 'expanded-token', got %q", cfg.Telegram.BotToken)
				}
				if cfg.WhatsApp.AccessToken != "expanded-access" {
					t.Errorf("Expected expanded access_token 'expanded-access', got %q", cfg.WhatsApp.AccessToken)
				}
			},
		},
		{
			name:    "invalid yaml returns error",
			yaml:    "not: valid: yaml: [",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "channels.yaml")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			if tt.yaml != "" {
				if err := os.WriteFile(path, []byte(tt.yaml), 0644); err != nil {
					t.Fatalf("Failed to write yaml: %v", err)
				}
			}

			cfg, err := config.LoadChannels(path)
			if tt.wantErr {
				if err == nil {
					t.Fatal("Expected error from invalid YAML, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadChannels() failed: %v", err)
			}
			if cfg == nil {
				t.Fatal("LoadChannels() returned nil")
			}
			tt.check(t, cfg)
		})
	}
}

func TestSaveChannels(t *testing.T) {
	roundTrip := &config.ChannelsConfig{
		Enabled:         true,
		MaxWorkers:      3,
		ImageRetention:  7,
		RequireApproval: false,
		Telegram: config.TelegramChannelConfig{
			Enabled:      true,
			BotToken:     "tok",
			AllowedUsers: []string{"42"},
			PollTimeout:  15,
		},
		WhatsApp: config.WhatsAppChannelConfig{
			Enabled:       true,
			PhoneNumberID: "pid",
			AccessToken:   "tok2",
			VerifyToken:   "verify",
			WebhookPort:   9090,
			AllowedUsers:  []string{"43"},
		},
	}

	tests := []struct {
		name  string
		path  []string
		cfg   *config.ChannelsConfig
		check func(t *testing.T, path string)
	}{
		{
			name: "round trip preserves fields",
			path: []string{"subdir", "channels.yaml"},
			cfg:  roundTrip,
			check: func(t *testing.T, path string) {
				loaded, err := config.LoadChannels(path)
				if err != nil {
					t.Fatalf("LoadChannels() failed: %v", err)
				}
				if loaded.Enabled != roundTrip.Enabled || loaded.MaxWorkers != roundTrip.MaxWorkers ||
					loaded.RequireApproval != roundTrip.RequireApproval || loaded.ImageRetention != roundTrip.ImageRetention {
					t.Errorf("Top-level fields mismatch: got %+v", loaded)
				}
				if loaded.Telegram.BotToken != roundTrip.Telegram.BotToken ||
					loaded.Telegram.PollTimeout != roundTrip.Telegram.PollTimeout ||
					len(loaded.Telegram.AllowedUsers) != 1 {
					t.Errorf("Telegram mismatch: got %+v", loaded.Telegram)
				}
				if loaded.WhatsApp.PhoneNumberID != roundTrip.WhatsApp.PhoneNumberID ||
					loaded.WhatsApp.WebhookPort != roundTrip.WhatsApp.WebhookPort {
					t.Errorf("WhatsApp mismatch: got %+v", loaded.WhatsApp)
				}
			},
		},
		{
			name: "creates parent directory",
			path: []string{"deeply", "nested", "channels.yaml"},
			cfg:  config.DefaultChannelsConfig(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(append([]string{t.TempDir()}, tt.path...)...)
			if err := config.SaveChannels(path, tt.cfg); err != nil {
				t.Fatalf("SaveChannels() failed: %v", err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("File not created at %q: %v", path, err)
			}
			if tt.check != nil {
				tt.check(t, path)
			}
		})
	}
}
