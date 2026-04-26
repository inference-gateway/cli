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

func TestLoadChannels_NonExistentFile(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "non-existent.yaml")

	cfg, err := config.LoadChannels(path)
	if err != nil {
		t.Fatalf("LoadChannels() should not error for missing file, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadChannels() returned nil")
	}
	defaults := config.DefaultChannelsConfig()
	if cfg.Enabled != defaults.Enabled || cfg.MaxWorkers != defaults.MaxWorkers {
		t.Errorf("Expected defaults, got %+v", cfg)
	}
}

func TestLoadChannels_ValidYAML(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "channels.yaml")

	yamlContent := `---
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
`
	if err := os.WriteFile(path, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write yaml: %v", err)
	}

	cfg, err := config.LoadChannels(path)
	if err != nil {
		t.Fatalf("LoadChannels() failed: %v", err)
	}
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

func TestLoadChannels_EnvironmentVariableExpansion(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "channels.yaml")

	t.Setenv("TEST_CHANNELS_BOT_TOKEN", "expanded-token")
	t.Setenv("TEST_CHANNELS_ACCESS_TOKEN", "expanded-access")

	yamlContent := `---
enabled: true
telegram:
  enabled: true
  bot_token: "${TEST_CHANNELS_BOT_TOKEN}"
whatsapp:
  access_token: "${TEST_CHANNELS_ACCESS_TOKEN}"
`
	if err := os.WriteFile(path, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write yaml: %v", err)
	}

	cfg, err := config.LoadChannels(path)
	if err != nil {
		t.Fatalf("LoadChannels() failed: %v", err)
	}
	if cfg.Telegram.BotToken != "expanded-token" {
		t.Errorf("Expected expanded bot_token 'expanded-token', got %q", cfg.Telegram.BotToken)
	}
	if cfg.WhatsApp.AccessToken != "expanded-access" {
		t.Errorf("Expected expanded access_token 'expanded-access', got %q", cfg.WhatsApp.AccessToken)
	}
}

func TestLoadChannels_InvalidYAML(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "channels.yaml")
	if err := os.WriteFile(path, []byte("not: valid: yaml: ["), 0644); err != nil {
		t.Fatalf("Failed to write yaml: %v", err)
	}

	if _, err := config.LoadChannels(path); err == nil {
		t.Fatal("Expected error from invalid YAML, got nil")
	}
}

func TestSaveChannels_RoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "subdir", "channels.yaml")

	cfg := &config.ChannelsConfig{
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

	if err := config.SaveChannels(path, cfg); err != nil {
		t.Fatalf("SaveChannels() failed: %v", err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("File was not created")
	}

	loaded, err := config.LoadChannels(path)
	if err != nil {
		t.Fatalf("LoadChannels() failed: %v", err)
	}
	if loaded.Enabled != cfg.Enabled || loaded.MaxWorkers != cfg.MaxWorkers ||
		loaded.RequireApproval != cfg.RequireApproval || loaded.ImageRetention != cfg.ImageRetention {
		t.Errorf("Top-level fields mismatch: got %+v", loaded)
	}
	if loaded.Telegram.BotToken != cfg.Telegram.BotToken ||
		loaded.Telegram.PollTimeout != cfg.Telegram.PollTimeout ||
		len(loaded.Telegram.AllowedUsers) != 1 {
		t.Errorf("Telegram mismatch: got %+v", loaded.Telegram)
	}
	if loaded.WhatsApp.PhoneNumberID != cfg.WhatsApp.PhoneNumberID ||
		loaded.WhatsApp.WebhookPort != cfg.WhatsApp.WebhookPort {
		t.Errorf("WhatsApp mismatch: got %+v", loaded.WhatsApp)
	}
}

func TestSaveChannels_CreatesParentDirectory(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "deeply", "nested", "channels.yaml")
	if err := config.SaveChannels(path, config.DefaultChannelsConfig()); err != nil {
		t.Fatalf("SaveChannels() failed: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("File not created at nested path: %v", err)
	}
}
