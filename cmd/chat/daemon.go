package chat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	uuid "github.com/google/uuid"

	version "github.com/inference-gateway/cli/cmd/version"
	config "github.com/inference-gateway/cli/config"
	clipboard "github.com/inference-gateway/cli/internal/computer/infrastructure/clipboard"
	container "github.com/inference-gateway/cli/internal/container"
	daemon "github.com/inference-gateway/cli/internal/daemon"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	streamevent "github.com/inference-gateway/cli/internal/platform/streamevent"
	telemetry "github.com/inference-gateway/cli/internal/platform/telemetry"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	daemonthread "github.com/inference-gateway/cli/internal/presentation/tui/daemonthread"
	colors "github.com/inference-gateway/cli/internal/presentation/tui/styles/colors"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

// attachTimeout bounds starting the daemon, reaching the session wherever it
// runs and listing its worker's models before the TUI opens.
const attachTimeout = 3 * time.Minute

// startDaemonChatSession runs the chat TUI as a client of the local daemon:
// every turn runs on the daemon's thread for this project, wherever that
// session lives, and the session keeps running when the TUI exits.
//
//nolint:funlen // attach steps print progress in order before the TUI opens
func startDaemonChatSession(cfg *config.Config, sessionID string, loadConfig func() (*config.Config, error)) error {
	_ = clipboard.Init()
	_ = streamevent.SetWriter(io.Discard)
	telemetry.ExecutionMode = telemetry.ExecInteractive

	cfg.Daemon.Binding.Enabled = true
	if err := daemon.FillBindingToken(cfg); err != nil {
		return err
	}
	projectDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolving the project dir: %w", err)
	}
	services := container.NewDaemonChatContainer(cfg, daemonthread.Options{ProjectDir: projectDir})
	services.EnableConfigReload(loadConfig)
	doShutdown := sync.OnceFunc(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := services.Shutdown(ctx); err != nil {
			logger.Error("error during shutdown", "error", err)
		}
	})
	defer doShutdown()
	utils.OnShutdownSignal(doShutdown)

	ctx, cancel := context.WithTimeout(context.Background(), attachTimeout)
	defer cancel()
	thread := services.GetDaemonThread()
	dim(fmt.Sprintf("• Connecting to infer daemon on %s…", cfg.Binding().Addr()))
	if err := thread.Connect(ctx); err != nil {
		return attachError(err)
	}

	mode := scheddomain.InheritedAgentMode()
	services.GetStateStore().SetAgentMode(mode)
	fresh := sessionID == ""
	if fresh {
		sessionID = uuid.NewString()
	}
	if err := thread.Follow(ctx, sessionID, sessionsdomain.ThreadOptions{Model: cfg.Agent.Model, Mode: mode.ModeKey()}); err != nil {
		return fmt.Errorf("opening session %s on infer daemon: %w (check it with: infer daemon environments)", sessionID, err)
	}
	dim(placementLine(thread.Placement(), sessionID, fresh))

	repo := services.GetConversationRepository()
	if !fresh {
		if err := repo.LoadConversation(ctx, sessionID); err != nil {
			dim("• Could not load the session's history: " + err.Error())
		}
	}
	if setter, ok := repo.(interface{ SetConversationID(string) }); ok && repo.GetCurrentConversationID() == "" {
		setter.SetConversationID(sessionID)
	}

	modelService := services.GetModelService()
	models, err := modelService.ListModels(ctx)
	if err != nil {
		return fmt.Errorf("listing the daemon worker's models: %w", err)
	}
	if len(models) == 0 {
		return errors.New("the daemon worker has no models available")
	}
	defaultModel := modelService.GetCurrentModel()
	if fresh {
		defaultModel = ""
		if cfg.Agent.Model != "" {
			defaultModel = validateAndSetDefaultModel(modelService, models, cfg.Agent.Model)
		}
	}

	return runChatProgram(services, cfg, models, defaultModel, version.GetVersionInfo(), func(id string) string {
		return daemonExitMessage(thread.Placement(), id)
	})
}

// attachError explains why the TUI could not reach its daemon.
func attachError(err error) error {
	return fmt.Errorf("could not reach infer daemon: %w (its daemon-<date>.log is in %s)", err, config.DefaultLogsDir())
}

// placementLine says where the session runs as the TUI attaches to it.
func placementLine(placement tui.SessionPlacement, sessionID string, fresh bool) string {
	short := sessionID
	if len(short) > 8 {
		short = short[:8]
	}
	switch {
	case placement.Remote():
		return fmt.Sprintf("• Session %s runs on %s, attaching through %s…", short, placement.Environment, placement.Local)
	case fresh:
		return fmt.Sprintf("• Starting session %s on %s…", short, placement.Where())
	}
	return fmt.Sprintf("• Session %s runs on %s…", short, placement.Where())
}

// daemonExitMessage tells the user the session outlives the TUI and how to
// come back to it.
func daemonExitMessage(placement tui.SessionPlacement, sessionID string) string {
	if sessionID == "" {
		return "Chat session ended."
	}
	return fmt.Sprintf("Chat session ended. The session keeps running in infer daemon (on %s). Reattach with: infer chat --daemon --session-id %s",
		placement.Where(), sessionID)
}

func dim(line string) {
	fmt.Println(colors.CreateColoredText(line, colors.DimColor))
}
