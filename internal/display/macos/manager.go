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
	stdin                io.Writer
	stdout               io.Reader
	stdinMutex           sync.Mutex
	approvalChans        map[string]chan domain.ApprovalAction
	approvalMutex        sync.RWMutex
	stopListener         chan struct{}
	listenerStopped      bool
	listenerStoppedMutex sync.Mutex
}

// NewFloatingWindowManager creates and starts a new floating window manager
func NewFloatingWindowManager(cfg *config.Config, eventBridge *EventBridge, stateManager domain.StateManager) (*FloatingWindowManager, error) {
	if runtime.GOOS != "darwin" {
		return &FloatingWindowManager{enabled: false}, nil
	}

	if !cfg.ComputerUse.Enabled || !cfg.ComputerUse.FloatingWindow.Enabled {
		return &FloatingWindowManager{enabled: false}, nil
	}

	mgr := &FloatingWindowManager{
		cfg:             cfg,
		eventBridge:     eventBridge,
		stateManager:    stateManager,
		enabled:         true,
		stopForward:     make(chan struct{}),
		approvalChans:   make(map[string]chan domain.ApprovalAction),
		stopListener:    make(chan struct{}),
		listenerStopped: false,
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
		buf := make([]byte, 1024)
		for {
			_, err := stderr.Read(buf)
			if err != nil {
				break
			}
		}
	}()

	mgr.stdin = stdin
	mgr.stdout = stdout
	mgr.cmd = cmd

	go mgr.startApprovalListener()

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
			if approvalEvent, ok := event.(domain.ToolApprovalRequestedEvent); ok {
				mgr.registerApprovalChannel(approvalEvent.ToolCall.Id, approvalEvent.ResponseChan)
			}

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

	if mgr.enabled && mgr.cfg.ComputerUse.FloatingWindow.RespawnOnClose {
		time.Sleep(1 * time.Second)

		if err := mgr.launchWindow(); err != nil {
			logger.Error("Failed to respawn floating window", "error", err)
			return
		}

		mgr.monitorWg.Add(1)
		go mgr.monitorProcess()
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

	mgr.stopApprovalListener()

	if err := mgr.shutdownProcess(); err != nil {
		return err
	}

	mgr.monitorWg.Wait()

	// Note: We don't delete the .app - it persists in .infer/ for future runs
	// This avoids re-extracting the embedded .app on every launch

	logger.Info("Floating window manager shutdown complete")

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

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	if _, err := fmt.Fprintf(mgr.stdin, "%s\n", data); err != nil {
		logger.Warn("Failed to write event to window", "error", err)
		return fmt.Errorf("failed to write to stdin: %w", err)
	}

	return nil
}

// startApprovalListener reads approval responses from the Swift process via stdout
func (mgr *FloatingWindowManager) startApprovalListener() {
	scanner := bufio.NewScanner(mgr.stdout)
	for scanner.Scan() {
		select {
		case <-mgr.stopListener:
			return
		default:
		}

		line := scanner.Text()

		if line == "" {
			continue
		}

		var response ApprovalResponse
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			logger.Warn("Failed to parse approval response", "error", err, "line", line)
			continue
		}

		mgr.handleApprovalResponse(response)
	}

	if err := scanner.Err(); err != nil {
		mgr.listenerStoppedMutex.Lock()
		if !mgr.listenerStopped {
			logger.Warn("Approval listener error", "error", err)
		}
		mgr.listenerStoppedMutex.Unlock()
	}
}

// handleApprovalResponse processes an approval response from the window
func (mgr *FloatingWindowManager) handleApprovalResponse(resp ApprovalResponse) {
	mgr.approvalMutex.Lock()
	defer mgr.approvalMutex.Unlock()

	ch, exists := mgr.approvalChans[resp.CallID]
	if !exists {
		logger.Warn("Received approval for unknown call ID", "call_id", resp.CallID, "known_call_ids", mgr.getCallIDs())
		return
	}

	logger.Info("Sending approval to channel", "call_id", resp.CallID, "action", resp.Action)

	select {
	case ch <- resp.Action:
		delete(mgr.approvalChans, resp.CallID)
		if mgr.stateManager != nil {
			mgr.stateManager.ClearApprovalUIState()
		}
	default:
		logger.Warn("Approval channel blocked", "call_id", resp.CallID)
	}
}

// registerApprovalChannel registers a response channel for a specific tool call
func (mgr *FloatingWindowManager) registerApprovalChannel(callID string, ch chan domain.ApprovalAction) {
	mgr.approvalMutex.Lock()
	defer mgr.approvalMutex.Unlock()

	mgr.approvalChans[callID] = ch
}

// getCallIDs returns a list of registered call IDs (for debugging)
func (mgr *FloatingWindowManager) getCallIDs() []string {
	ids := make([]string, 0, len(mgr.approvalChans))
	for id := range mgr.approvalChans {
		ids = append(ids, id)
	}
	return ids
}

// stopApprovalListener signals the approval listener to stop
func (mgr *FloatingWindowManager) stopApprovalListener() {
	mgr.listenerStoppedMutex.Lock()
	defer mgr.listenerStoppedMutex.Unlock()

	if !mgr.listenerStopped {
		close(mgr.stopListener)
		mgr.listenerStopped = true
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
