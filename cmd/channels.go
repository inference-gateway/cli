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
	heartbeat "github.com/inference-gateway/cli/internal/services/heartbeat"
	scheduler "github.com/inference-gateway/cli/internal/services/scheduler"
	stt "github.com/inference-gateway/cli/internal/stt"
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
config.yaml is no longer read - re-run 'infer init' to migrate it.

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

// RunChannelsCommand starts the channel listener daemon. The daemon
// hosts up to three subsystems - channels, scheduler, and heartbeat -
// and starts whichever are enabled. At least one must be enabled or
// the daemon refuses to boot (otherwise it would just sleep forever).
func RunChannelsCommand(cfg *config.Config) error {
	if !cfg.Channels.Enabled && !cfg.Tools.Schedule.Enabled && !cfg.Heartbeat.Enabled {
		return fmt.Errorf("nothing to run: enable at least one of channels, scheduler, or heartbeat in .infer/")
	}

	cm := services.NewChannelManagerService(cfg.Channels)

	if cfg.Channels.Enabled {
		if err := registerChannels(cm, cfg); err != nil {
			return err
		}
	}

	logger.Info("Starting channels-manager", "version", version)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	if cfg.Channels.Enabled {
		logger.Info("starting channel listener...")
		if err := cm.Start(ctx); err != nil {
			return fmt.Errorf("failed to start channels: %w", err)
		}
	}

	sched, err := startScheduler(ctx, cm, cfg)
	if err != nil {
		_ = cm.Stop()
		return fmt.Errorf("failed to start scheduler: %w", err)
	}

	hb, err := startHeartbeat(ctx, cfg)
	if err != nil {
		if sched != nil {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
			_ = sched.Stop(stopCtx)
			stopCancel()
		}
		_ = cm.Stop()
		return fmt.Errorf("failed to start heartbeat: %w", err)
	}

	logger.Info("daemon ready. Press Ctrl+C to stop.")

	<-sigChan
	logger.Info("shutting down...")
	cancel()

	if hb != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := hb.Stop(stopCtx); err != nil {
			logger.Error("Failed to stop heartbeat", "error", err)
		}
		stopCancel()
	}

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

	logger.Info("daemon stopped.")
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
	return svc, nil
}

// startHeartbeat initialises the heartbeat service when enabled.
// Returns nil service when disabled. Parses interval/initial_delay as
// time.Duration strings and surfaces parse errors so the daemon fails
// fast on bad config.
func startHeartbeat(ctx context.Context, cfg *config.Config) (*heartbeat.Service, error) {
	if !cfg.Heartbeat.Enabled {
		return nil, nil
	}

	interval, err := time.ParseDuration(cfg.Heartbeat.Interval)
	if err != nil {
		return nil, fmt.Errorf("parse heartbeat.interval %q: %w", cfg.Heartbeat.Interval, err)
	}

	var initialDelay time.Duration
	if cfg.Heartbeat.InitialDelay != "" {
		initialDelay, err = time.ParseDuration(cfg.Heartbeat.InitialDelay)
		if err != nil {
			return nil, fmt.Errorf("parse heartbeat.initial_delay %q: %w", cfg.Heartbeat.InitialDelay, err)
		}
	}

	prompt := cfg.Heartbeat.Prompt
	if prompt == "" {
		prompt = config.DefaultHeartbeatConfig().Prompt
	}

	svc, err := heartbeat.NewService(heartbeat.Options{
		Config: heartbeat.Config{
			Interval:     interval,
			InitialDelay: initialDelay,
			Model:        cfg.Heartbeat.Model,
			Prompt:       prompt,
		},
	})
	if err != nil {
		return nil, err
	}
	if err := svc.Start(ctx); err != nil {
		return nil, err
	}
	return svc, nil
}

// registerChannels registers enabled channel implementations with the manager
func registerChannels(cm *services.ChannelManagerService, cfg *config.Config) error {
	registered := 0

	if cfg.Channels.Telegram.Enabled {
		var transcriber channels.VoiceTranscriber
		if cfg.SpeechToText.Enabled {
			transcriber = stt.NewFileTranscriber(cfg.SpeechToText)
			logger.Info("Speech-to-text enabled for inbound voice messages", "model", cfg.SpeechToText.Model)
		}
		telegramCh := channels.NewTelegramChannel(cfg.Channels.Telegram, transcriber)
		cm.Register(telegramCh)
		registered++
		logger.Info("registered channel", "channel", "telegram")
	}

	// WhatsApp channel is not yet implemented; enable this block once
	// channels.NewWhatsAppChannel exists.
	// if cfg.Channels.WhatsApp.Enabled {
	// 	whatsappCh := channels.NewWhatsAppChannel(cfg.Channels.WhatsApp)
	// 	cm.Register(whatsappCh)
	// 	registered++
	// 	logger.Info("registered channel", "channel", "whatsapp")
	// }

	if registered == 0 {
		return fmt.Errorf("no channels are enabled. Enable at least one channel in config")
	}

	return nil
}

func init() {
	rootCmd.AddCommand(channelsCmd)
}
