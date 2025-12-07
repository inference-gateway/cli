package services

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	uuid "github.com/google/uuid"
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// BackgroundShellService manages background shell execution
type BackgroundShellService struct {
	shellTracker  domain.ShellTracker
	config        *config.Config
	eventChannel  chan<- domain.ChatEvent
	cleanupTicker *time.Ticker
	stopCleanup   chan struct{}
	wg            sync.WaitGroup
	mutex         sync.RWMutex
}

// NewBackgroundShellService creates a new background shell service
func NewBackgroundShellService(
	tracker domain.ShellTracker,
	cfg *config.Config,
	eventChannel chan<- domain.ChatEvent,
) *BackgroundShellService {
	service := &BackgroundShellService{
		shellTracker: tracker,
		config:       cfg,
		eventChannel: eventChannel,
		stopCleanup:  make(chan struct{}),
	}

	service.startCleanupRoutine()

	return service
}

// DetachToBackground moves a running command to background
func (s *BackgroundShellService) DetachToBackground(
	ctx context.Context,
	cmd *exec.Cmd,
	command string,
	outputBuffer domain.OutputRingBuffer,
) (string, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if !s.config.Tools.Bash.BackgroundShells.Enabled {
		return "", fmt.Errorf("background shells are disabled in configuration")
	}

	shellID := generateShellID()

	shellCtx, cancel := context.WithCancel(context.Background())

	shell := &domain.BackgroundShell{
		ShellID:      shellID,
		Command:      command,
		Cmd:          cmd,
		StartedAt:    time.Now(),
		State:        domain.ShellStateRunning,
		OutputBuffer: outputBuffer,
		CancelFunc:   cancel,
		ReadOffset:   0,
	}

	if err := s.shellTracker.Add(shell); err != nil {
		cancel()
		return "", fmt.Errorf("failed to add shell to tracker: %w", err)
	}

	s.wg.Add(1)
	go s.monitorShell(shellCtx, shell)

	if s.eventChannel != nil {
		select {
		case s.eventChannel <- domain.ShellDetachedEvent{
			RequestID: "system",
			Timestamp: time.Now(),
			ShellID:   shellID,
			Command:   command,
		}:
		default:
			logger.Warn("Event channel full, shell detached event dropped", "shell_id", shellID)
		}
	}

	logger.Info("Shell detached to background", "shell_id", shellID, "command", command)

	return shellID, nil
}

// monitorShell monitors a background shell until completion
func (s *BackgroundShellService) monitorShell(ctx context.Context, shell *domain.BackgroundShell) {
	defer s.wg.Done()

	err := shell.Cmd.Wait()

	completedAt := time.Now()
	duration := completedAt.Sub(shell.StartedAt)

	shell.CompletedAt = &completedAt

	if err != nil {
		s.handleShellFailure(shell, err)
		return
	}

	{
		exitCode := 0
		shell.ExitCode = &exitCode
		shell.State = domain.ShellStateCompleted

		logger.Info("Background shell completed", "shell_id", shell.ShellID, "duration", duration)

		if s.eventChannel != nil {
			select {
			case s.eventChannel <- domain.ShellCompletedEvent{
				RequestID: "system",
				Timestamp: time.Now(),
				ShellID:   shell.ShellID,
				ExitCode:  exitCode,
				Duration:  duration,
			}:
			default:
				logger.Warn("Event channel full, shell completed event dropped", "shell_id", shell.ShellID)
			}
		}
	}
}

// handleShellFailure handles a failed shell execution
func (s *BackgroundShellService) handleShellFailure(shell *domain.BackgroundShell, err error) {
	exitCode := -1
	if exitErr, ok := err.(*exec.ExitError); ok {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			exitCode = status.ExitStatus()
		}
	}

	shell.ExitCode = &exitCode
	shell.State = domain.ShellStateFailed

	logger.Error("Background shell failed", "shell_id", shell.ShellID, "error", err, "exit_code", exitCode)

	if s.eventChannel == nil {
		return
	}

	select {
	case s.eventChannel <- domain.ShellFailedEvent{
		RequestID: "system",
		Timestamp: time.Now(),
		ShellID:   shell.ShellID,
		Error:     err.Error(),
		ExitCode:  exitCode,
	}:
	default:
		logger.Warn("Event channel full, shell failed event dropped", "shell_id", shell.ShellID)
	}
}

// GetShellOutput retrieves incremental output from a shell
func (s *BackgroundShellService) GetShellOutput(shellID string, fromOffset int64) (string, int64, domain.ShellState, error) {
	shell := s.shellTracker.Get(shellID)
	if shell == nil {
		return "", 0, "", fmt.Errorf("shell not found: %s", shellID)
	}

	offset := fromOffset
	if offset < 0 {
		offset = shell.ReadOffset
	}

	output, newOffset := shell.OutputBuffer.ReadFrom(offset)

	shell.ReadOffset = newOffset

	return output, newOffset, shell.State, nil
}

// GetShellOutputWithFilter retrieves output with optional regex filtering
func (s *BackgroundShellService) GetShellOutputWithFilter(shellID string, fromOffset int64, filterPattern string) (string, int64, domain.ShellState, error) {
	output, newOffset, state, err := s.GetShellOutput(shellID, fromOffset)
	if err != nil {
		return "", 0, "", err
	}

	if filterPattern == "" {
		return output, newOffset, state, nil
	}

	re, err := regexp.Compile(filterPattern)
	if err != nil {
		return "", 0, "", fmt.Errorf("invalid filter pattern: %w", err)
	}

	lines := strings.Split(output, "\n")
	var filteredLines []string

	for _, line := range lines {
		if re.MatchString(line) {
			filteredLines = append(filteredLines, line)
		}
	}

	filteredOutput := strings.Join(filteredLines, "\n")

	return filteredOutput, newOffset, state, nil
}

// CancelShell cancels a running background shell
func (s *BackgroundShellService) CancelShell(shellID string) error {
	shell := s.shellTracker.Get(shellID)
	if shell == nil {
		return fmt.Errorf("shell not found: %s", shellID)
	}

	if shell.State != domain.ShellStateRunning {
		return fmt.Errorf("shell is not running (state: %s)", shell.State)
	}

	logger.Info("Cancelling background shell", "shell_id", shellID)

	if shell.Cmd.Process != nil {
		if err := shell.Cmd.Process.Signal(syscall.SIGTERM); err != nil {
			logger.Warn("Failed to send SIGTERM", "shell_id", shellID, "error", err)
		}

		done := make(chan struct{})
		go func() {
			_ = shell.Cmd.Wait()
			close(done)
		}()

		select {
		case <-done:
			logger.Info("Shell exited gracefully", "shell_id", shellID)
		case <-time.After(5 * time.Second):
			logger.Warn("Shell did not exit gracefully, sending SIGKILL", "shell_id", shellID)
			if err := shell.Cmd.Process.Kill(); err != nil {
				logger.Error("Failed to kill shell", "shell_id", shellID, "error", err)
			}
		}
	}

	if shell.CancelFunc != nil {
		shell.CancelFunc()
	}

	completedAt := time.Now()
	shell.CompletedAt = &completedAt
	shell.State = domain.ShellStateCancelled

	if s.eventChannel != nil {
		select {
		case s.eventChannel <- domain.ShellCancelledEvent{
			RequestID: "system",
			Timestamp: time.Now(),
			ShellID:   shellID,
		}:
		default:
			logger.Warn("Event channel full, shell cancelled event dropped", "shell_id", shellID)
		}
	}

	return nil
}

// GetAllShells returns all tracked shells
func (s *BackgroundShellService) GetAllShells() []*domain.BackgroundShell {
	return s.shellTracker.GetAll()
}

// GetShell returns a specific shell by ID
func (s *BackgroundShellService) GetShell(shellID string) *domain.BackgroundShell {
	return s.shellTracker.Get(shellID)
}

// RemoveShell removes a shell from tracking
func (s *BackgroundShellService) RemoveShell(shellID string) error {
	return s.shellTracker.Remove(shellID)
}

// GetStats returns statistics about background shells
func (s *BackgroundShellService) GetStats() map[string]int {
	all := s.shellTracker.GetAll()

	stats := map[string]int{
		"total":     len(all),
		"running":   0,
		"completed": 0,
		"failed":    0,
		"cancelled": 0,
	}

	for _, shell := range all {
		switch shell.State {
		case domain.ShellStateRunning:
			stats["running"]++
		case domain.ShellStateCompleted:
			stats["completed"]++
		case domain.ShellStateFailed:
			stats["failed"]++
		case domain.ShellStateCancelled:
			stats["cancelled"]++
		}
	}

	return stats
}

// startCleanupRoutine starts the periodic cleanup of old completed shells
func (s *BackgroundShellService) startCleanupRoutine() {
	s.cleanupTicker = time.NewTicker(10 * time.Minute)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		for {
			select {
			case <-s.cleanupTicker.C:
				s.performCleanup()
			case <-s.stopCleanup:
				s.cleanupTicker.Stop()
				return
			}
		}
	}()
}

// performCleanup removes old completed/failed/cancelled shells
func (s *BackgroundShellService) performCleanup() {
	retentionDuration := time.Duration(s.config.Tools.Bash.BackgroundShells.RetentionMinutes) * time.Minute

	removed := s.shellTracker.Cleanup(retentionDuration)

	if removed > 0 {
		logger.Info("Cleaned up old background shells", "removed", removed, "retention", retentionDuration)
	}
}

// Stop stops the background shell service and cleanup routine
func (s *BackgroundShellService) Stop() {
	logger.Info("Stopping background shell service")

	close(s.stopCleanup)

	s.wg.Wait()

	logger.Info("Background shell service stopped")
}

// generateShellID generates a unique shell ID
func generateShellID() string {
	return "shell-" + uuid.New().String()[:8]
}
