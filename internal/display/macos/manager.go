//go:build darwin

package macos

import (
	"bufio"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

//go:embed ComputerUse/build/ComputerUse.app
var computerUseApp embed.FS

// FloatingWindowManager manages the lifecycle of the floating progress window
type FloatingWindowManager struct {
	cfg          *config.Config
	eventBridge  *EventBridge
	stateManager domain.StateManager
	cmd          *exec.Cmd
	enabled      bool
	eventSub     chan domain.ChatEvent
	stopForward  chan struct{}
	appPath      string
	monitorWg    sync.WaitGroup
	// IPC fields (merged from ProcessManager)
	stdin        io.Writer
	stdout       io.Reader
	stdinMutex   sync.Mutex
	agentService domain.AgentService
	// Pause/resume listener
	pauseListener        chan struct{}
	pauseListenerStopped bool
	pauseStopMutex       sync.Mutex
}

// NewFloatingWindowManager creates and starts a new floating window manager
func NewFloatingWindowManager(
	cfg *config.Config,
	eventBridge *EventBridge,
	stateManager domain.StateManager,
	agentService domain.AgentService,
) (*FloatingWindowManager, error) {
	if runtime.GOOS != "darwin" {
		return &FloatingWindowManager{enabled: false}, nil
	}

	if !cfg.ComputerUse.Enabled || !cfg.ComputerUse.FloatingWindow.Enabled {
		return &FloatingWindowManager{enabled: false}, nil
	}

	mgr := &FloatingWindowManager{
		cfg:                  cfg,
		eventBridge:          eventBridge,
		stateManager:         stateManager,
		agentService:         agentService,
		enabled:              true,
		stopForward:          make(chan struct{}),
		pauseListener:        make(chan struct{}),
		pauseListenerStopped: false,
	}

	if err := mgr.launchWindow(); err != nil {
		return nil, fmt.Errorf("failed to launch floating window: %w", err)
	}

	mgr.eventSub = eventBridge.Subscribe()
	go mgr.forwardEvents()

	mgr.monitorWg.Add(1)
	go mgr.monitorProcess()

	if cfg.ComputerUse.Screenshot.ShowOverlay {
		time.Sleep(200 * time.Millisecond)
		if err := mgr.ShowBorderOverlay(); err != nil {
			logger.Warn("Failed to show border overlay", "error", err)
		}
	}

	return mgr, nil
}

// launchWindow starts the Swift window process
func (mgr *FloatingWindowManager) launchWindow() error {
	appDir := filepath.Join(mgr.cfg.GetConfigDir(), "tmp", "ComputerUse.app")
	mgr.appPath = appDir

	if _, err := os.Stat(appDir); os.IsNotExist(err) {
		logger.Debug("Extracting ComputerUse.app from embedded binary", "path", appDir)
		if err := mgr.extractApp(appDir); err != nil {
			return fmt.Errorf("failed to extract embedded app: %w", err)
		}
		logger.Info("ComputerUse.app extracted successfully", "path", appDir)
	}

	position := mgr.cfg.ComputerUse.FloatingWindow.Position
	alwaysOnTop := fmt.Sprintf("%t", mgr.cfg.ComputerUse.FloatingWindow.AlwaysOnTop)

	executablePath := filepath.Join(appDir, "Contents", "MacOS", "ComputerUse")
	cmd := exec.Command(executablePath, position, alwaysOnTop)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ComputerUse.app: %w", err)
	}

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			logger.Debug("ComputerUse stderr", "output", line)
		}
	}()

	mgr.stdin = stdin
	mgr.stdout = stdout
	mgr.cmd = cmd

	go mgr.startPauseResumeListener()

	return nil
}

// extractApp extracts the embedded .app bundle to the target directory
func (mgr *FloatingWindowManager) extractApp(targetDir string) error {
	const appPrefix = "ComputerUse/build/ComputerUse.app"

	return fs.WalkDir(computerUseApp, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if path == "." {
			return nil
		}

		if !strings.HasPrefix(path, appPrefix) {
			return nil
		}

		if path == appPrefix {
			return nil
		}

		relPath := path[len(appPrefix)+1:]

		targetPath := filepath.Join(targetDir, relPath)

		if d.IsDir() {
			return os.MkdirAll(targetPath, 0755)
		}

		data, err := computerUseApp.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read embedded file %s: %w", path, err)
		}

		perm := os.FileMode(0644)
		if filepath.Base(filepath.Dir(targetPath)) == "MacOS" {
			perm = 0755
		}

		if err := os.WriteFile(targetPath, data, perm); err != nil {
			return fmt.Errorf("failed to write file %s: %w", targetPath, err)
		}

		return nil
	})
}

// forwardEvents forwards chat events from the EventBridge to the Swift window
func (mgr *FloatingWindowManager) forwardEvents() {
	for {
		select {
		case event := <-mgr.eventSub:
			if err := mgr.writeEvent(event); err != nil {
				logger.Warn("Failed to forward event to window", "error", err)
			}

		case <-mgr.stopForward:
			return
		}
	}
}

// monitorProcess watches the Swift process and respawns if configured
func (mgr *FloatingWindowManager) monitorProcess() {
	defer mgr.monitorWg.Done()

	if mgr.cmd == nil {
		return
	}

	err := mgr.cmd.Wait()
	if err != nil {
		logger.Error("Swift process exited", "error", err)
	}

	if !mgr.enabled || !mgr.cfg.ComputerUse.FloatingWindow.RespawnOnClose {
		return
	}

	mgr.respawnWindow()
}

// respawnWindow handles respawning the floating window after crash/close
func (mgr *FloatingWindowManager) respawnWindow() {
	logger.Debug("Respawning floating window after crash/close")
	time.Sleep(1 * time.Second)

	if err := mgr.launchWindow(); err != nil {
		logger.Error("Failed to respawn floating window", "error", err)
		return
	}

	mgr.restoreBorderOverlay()

	mgr.monitorWg.Add(1)
	go mgr.monitorProcess()
}

// restoreBorderOverlay restores the border overlay if it was enabled
func (mgr *FloatingWindowManager) restoreBorderOverlay() {
	if !mgr.cfg.ComputerUse.Screenshot.ShowOverlay {
		return
	}

	time.Sleep(200 * time.Millisecond)
	if err := mgr.ShowBorderOverlay(); err != nil {
		logger.Warn("Failed to show border overlay after respawn", "error", err)
	} else {
		logger.Info("Border overlay restored after respawn")
	}
}

// Shutdown gracefully shuts down the floating window manager
func (mgr *FloatingWindowManager) Shutdown() error {
	if !mgr.enabled {
		return nil
	}

	logger.Info("Shutting down floating window manager")

	mgr.enabled = false

	close(mgr.stopForward)

	if mgr.eventSub != nil {
		mgr.eventBridge.Unsubscribe(mgr.eventSub)
	}

	mgr.stopPauseResumeListener()

	if err := mgr.shutdownProcess(); err != nil {
		return err
	}

	mgr.monitorWg.Wait()

	if mgr.eventBridge != nil {
		mgr.eventBridge.Close()
	}

	return nil
}

// shutdownProcess terminates the Swift process gracefully
func (mgr *FloatingWindowManager) shutdownProcess() error {
	if mgr.cmd == nil || mgr.cmd.Process == nil {
		return nil
	}

	return mgr.sendTermSignal()
}

// sendTermSignal sends SIGTERM to the Swift process, falls back to SIGKILL if needed
func (mgr *FloatingWindowManager) sendTermSignal() error {
	if err := mgr.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		if killErr := mgr.cmd.Process.Kill(); killErr != nil {
			logger.Warn("Failed to kill Swift process", "error", killErr)
			return fmt.Errorf("failed to kill process: %w", killErr)
		}
	}
	return nil
}

// writeEvent sends an event to the Swift process via stdin
func (mgr *FloatingWindowManager) writeEvent(event domain.ChatEvent) error {
	mgr.stdinMutex.Lock()
	defer mgr.stdinMutex.Unlock()

	if !mgr.enabled {
		return fmt.Errorf("manager is disabled (shutting down)")
	}

	if mgr.cmd == nil || mgr.cmd.Process == nil {
		return fmt.Errorf("process not running")
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	if progressEvent, ok := event.(domain.ToolExecutionProgressEvent); ok {
		if progressEvent.ToolName == "GetLatestScreenshot" && progressEvent.Status == "completed" {
			jsonPreview := string(data)
			if len(jsonPreview) > 500 {
				jsonPreview = jsonPreview[:500] + "..."
			}
			logger.Info("Sending GetLatestScreenshot completed event to Swift",
				"hasImages", len(progressEvent.Images) > 0,
				"imageCount", len(progressEvent.Images),
				"jsonLength", len(data),
				"jsonPreview", jsonPreview)
		}
	}

	if _, err := fmt.Fprintf(mgr.stdin, "%s\n", data); err != nil {
		logger.Warn("Failed to write event to window", "error", err)
		return fmt.Errorf("failed to write to stdin: %w", err)
	}

	return nil
}

// startPauseResumeListener reads pause/resume requests from the Swift process via stdout
func (mgr *FloatingWindowManager) startPauseResumeListener() {
	logger.Debug("startPauseResumeListener started")
	scanner := bufio.NewScanner(mgr.stdout)
	for scanner.Scan() {
		select {
		case <-mgr.pauseListener:
			logger.Debug("Pause listener stopped")
			return
		default:
		}

		line := scanner.Text()
		logger.Debug("Pause listener received line", "line", line)
		if line == "" {
			continue
		}

		var request PauseResumeRequest
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			logger.Warn("Failed to parse pause/resume request", "error", err, "line", line)
			continue
		}

		logger.Debug("Pause listener parsed request", "action", request.Action, "request_id", request.RequestID)
		mgr.handlePauseResumeRequest(request)
	}

	if err := scanner.Err(); err != nil {
		mgr.pauseStopMutex.Lock()
		if !mgr.pauseListenerStopped {
			logger.Warn("Pause/resume listener error", "error", err)
		}
		mgr.pauseStopMutex.Unlock()
	}
}

// handlePauseResumeRequest processes a pause or resume request from the window
func (mgr *FloatingWindowManager) handlePauseResumeRequest(req PauseResumeRequest) {
	switch req.Action {
	case "pause":
		event := domain.ComputerUsePausedEvent{
			RequestID: req.RequestID,
			Timestamp: time.Now(),
		}
		mgr.eventBridge.Publish(event)

	case "resume":
		event := domain.ComputerUseResumedEvent{
			RequestID: req.RequestID,
			Timestamp: time.Now(),
		}
		mgr.eventBridge.Publish(event)

	default:
		logger.Warn("Unknown pause/resume action", "action", req.Action)
	}
}

// stopPauseResumeListener signals the pause/resume listener to stop
func (mgr *FloatingWindowManager) stopPauseResumeListener() {
	mgr.pauseStopMutex.Lock()
	defer mgr.pauseStopMutex.Unlock()

	if !mgr.pauseListenerStopped {
		close(mgr.pauseListener)
		mgr.pauseListenerStopped = true
	}
}

// ShowBorderOverlay sends an event to show the blue border around the screen
func (mgr *FloatingWindowManager) ShowBorderOverlay() error {
	if !mgr.enabled {
		return nil
	}

	event := domain.BorderOverlayEvent{
		BaseChatEvent: domain.BaseChatEvent{
			RequestID: "border-show",
			Timestamp: time.Now(),
		},
		BorderAction: "show",
	}

	return mgr.writeEvent(event)
}

// HideBorderOverlay sends an event to hide the blue border around the screen
func (mgr *FloatingWindowManager) HideBorderOverlay() error {
	if !mgr.enabled {
		return nil
	}

	event := domain.BorderOverlayEvent{
		BaseChatEvent: domain.BaseChatEvent{
			RequestID: "border-hide",
			Timestamp: time.Now(),
		},
		BorderAction: "hide",
	}

	return mgr.writeEvent(event)
}

// ShowClickIndicator sends an event to show a visual click indicator at the given coordinates
func (mgr *FloatingWindowManager) ShowClickIndicator(x, y int) error {
	if !mgr.enabled {
		return nil
	}

	event := domain.ClickIndicatorEvent{
		BaseChatEvent: domain.BaseChatEvent{
			RequestID: "click-indicator",
			Timestamp: time.Now(),
		},
		X:              x,
		Y:              y,
		ClickIndicator: true,
	}

	return mgr.writeEvent(event)
}
