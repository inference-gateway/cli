package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	config "github.com/inference-gateway/cli/config"
	services "github.com/inference-gateway/cli/internal/services"
	channels "github.com/inference-gateway/cli/internal/services/channels"
	cobra "github.com/spf13/cobra"
)

var channelsCmd = &cobra.Command{
	Use:   "channels-manager",
	Short: "Start the channel listener for remote messaging platforms",
	Long: `Start a long-running daemon that listens for messages from external platforms
(e.g., Telegram) and triggers the agent for each incoming message.

Each message spawns a new agent invocation with a deterministic session ID per sender,
so conversations persist across messages. The agent runs autonomously, and the response
is sent back through the originating channel.

Configuration is done via .infer/config.yaml or environment variables.

Examples:
  # Start listening for Telegram messages
  infer channels-manager

  # With environment variables
  INFER_CHANNELS_ENABLED=true \
  INFER_CHANNELS_TELEGRAM_ENABLED=true \
  INFER_CHANNELS_TELEGRAM_BOT_TOKEN="your-token" \
  INFER_CHANNELS_TELEGRAM_ALLOWED_USERS="123456789" \
  infer channels-manager`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := getConfigFromViper()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		return RunChannelsCommand(cfg)
	},
}

// RunChannelsCommand starts the channel listener daemon
func RunChannelsCommand(cfg *config.Config) error {
	if !cfg.Channels.Enabled {
		return fmt.Errorf("channels are not enabled. Set channels.enabled: true in config or INFER_CHANNELS_ENABLED=true")
	}

	cm := services.NewChannelManagerService(cfg.Channels)

	if err := registerChannels(cm, cfg); err != nil {
		return err
	}

	fmt.Println("Starting channel listener...")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	if err := cm.Start(ctx); err != nil {
		return fmt.Errorf("failed to start channels: %w", err)
	}

	fmt.Println("Listening for messages. Press Ctrl+C to stop.")

	<-sigChan
	fmt.Println("\nShutting down channels...")
	cancel()

	if err := cm.Stop(); err != nil {
		return fmt.Errorf("failed to stop channels: %w", err)
	}

	fmt.Println("Channels stopped.")
	return nil
}

// registerChannels registers enabled channel implementations with the manager
func registerChannels(cm *services.ChannelManagerService, cfg *config.Config) error {
	registered := 0

	if cfg.Channels.Telegram.Enabled {
		telegramCh := channels.NewTelegramChannel(cfg.Channels.Telegram)
		cm.Register(telegramCh)
		registered++
		fmt.Println("Registered Telegram channel")
	}

	if registered == 0 {
		return fmt.Errorf("no channels are enabled. Enable at least one channel in config")
	}

	return nil
}

func init() {
	rootCmd.AddCommand(channelsCmd)
}
