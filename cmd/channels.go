package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/logger"
	services "github.com/inference-gateway/cli/internal/services"
	channels "github.com/inference-gateway/cli/internal/services/channels"
	scheduler "github.com/inference-gateway/cli/internal/services/scheduler"
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

Configuration is done via .infer/channels.yaml (seeded by 'infer init') or
INFER_CHANNELS_* environment variables. The legacy 'channels:' block in
config.yaml is no longer read — re-run 'infer init' to migrate it.

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
		return RunChannelsCommand(Cfg)
	},
}

// RunChannelsCommand starts the channel listener daemon
func RunChannelsCommand(cfg *config.Config) error {
	if !cfg.Channels.Enabled {
		return fmt.Errorf("channels are not enabled. Set enabled: true in .infer/channels.yaml or INFER_CHANNELS_ENABLED=true")
	}

	cm := services.NewChannelManagerService(cfg.Channels)

	if err := registerChannels(cm, cfg); err != nil {
		return err
	}

	logger.Info("Starting channel listener...")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	if err := cm.Start(ctx); err != nil {
		return fmt.Errorf("failed to start channels: %w", err)
	}

	sched, err := startScheduler(ctx, cm, cfg)
	if err != nil {
		_ = cm.Stop()
		return fmt.Errorf("failed to start scheduler: %w", err)
	}

	logger.Info("Listening for messages. Press Ctrl+C to stop.")

	<-sigChan
	logger.Info("Shutting down channels...")
	cancel()

	if sched != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := sched.Stop(stopCtx); err != nil {
			logger.Error("Failed to stop scheduler", "error", err)
		}
		stopCancel()
	}

	if err := cm.Stop(); err != nil {
		return fmt.Errorf("failed to stop channels: %w", err)
	}

	logger.Info("Channels stopped.")
	return nil
}

// startScheduler initialises the schedule scheduler service when the schedule
// tool is enabled. Returns nil scheduler when disabled.
func startScheduler(ctx context.Context, cm *services.ChannelManagerService, cfg *config.Config) (*scheduler.Service, error) {
	if !cfg.Tools.Schedule.Enabled {
		return nil, nil
	}
	dir := cfg.Tools.Schedule.StorageDir
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home dir: %w", err)
		}
		dir = filepath.Join(home, config.ConfigDirName, "schedules")
	}
	store, err := scheduler.NewStore(dir)
	if err != nil {
		return nil, err
	}
	svc, err := scheduler.NewService(scheduler.Options{
		Store:         store,
		ChannelLookup: cm.GetChannel,
	})
	if err != nil {
		return nil, err
	}
	if err := svc.Start(ctx); err != nil {
		return nil, err
	}
	logger.Info("Scheduler started", "storage_dir", dir)
	return svc, nil
}

// registerChannels registers enabled channel implementations with the manager
func registerChannels(cm *services.ChannelManagerService, cfg *config.Config) error {
	registered := 0

	if cfg.Channels.Telegram.Enabled {
		telegramCh := channels.NewTelegramChannel(cfg.Channels.Telegram)
		cm.Register(telegramCh)
		registered++
		logger.Info("Registered channel", "channel", "telegram")
	}

	// WhatsApp channel is not yet implemented; enable this block once
	// channels.NewWhatsAppChannel exists.
	// if cfg.Channels.WhatsApp.Enabled {
	// 	whatsappCh := channels.NewWhatsAppChannel(cfg.Channels.WhatsApp)
	// 	cm.Register(whatsappCh)
	// 	registered++
	// 	logger.Info("Registered channel", "channel", "whatsapp")
	// }

	if registered == 0 {
		return fmt.Errorf("no channels are enabled. Enable at least one channel in config")
	}

	return nil
}

func init() {
	rootCmd.AddCommand(channelsCmd)
}
