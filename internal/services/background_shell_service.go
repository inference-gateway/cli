package services

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	uuid "github.com/google/uuid"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	jobs "github.com/inference-gateway/cli/internal/services/jobs"
)

// BackgroundShellService is the thin front-end for background bash shells: it
// detaches a running command (registering it so the bash tools can read its
// output), submits a shellJob to the supervisor (which monitors it, notifies the
// agent, and reaps it), and brokers output retrieval and cancellation. The
// completion monitoring and reaping that used to live here are now the
// supervisor's job.
type BackgroundShellService struct {
	shellTracker domain.ShellTracker
	supervisor   *jobs.Supervisor
	config       *config.Config
	eventChannel chan<- domain.ChatEvent
	mutex        sync.RWMutex
}

// NewBackgroundShellService creates a new background shell service. supervisor
// monitors and reaps each detached shell.
func NewBackgroundShellService(
	tracker domain.ShellTracker,
	supervisor *jobs.Supervisor,
	cfg *config.Config,
	eventChannel chan<- domain.ChatEvent,
) *BackgroundShellService {
	return &BackgroundShellService{
		shellTracker: tracker,
		supervisor:   supervisor,
		config:       cfg,
		eventChannel: eventChannel,
	}
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

	shell := &domain.BackgroundShell{
		ShellID:      shellID,
		Command:      command,
		Cmd:          cmd,
		StartedAt:    time.Now(),
		State:        domain.ShellStateRunning,
		OutputBuffer: outputBuffer,
		ReadOffset:   0,
	}

	if err := s.shellTracker.Add(shell); err != nil {
		return "", fmt.Errorf("failed to add shell to tracker: %w", err)
	}

	s.supervisor.Submit(jobs.NewShellJob(shell, s.shellTracker))

	if s.eventChannel != nil {
		select {
		case s.eventChannel <- domain.ShellDetachedEvent{
			RequestID: "system",
			Timestamp: time.Now(),
			ShellID:   shellID,
			Command:   command,
		}:
		default:
			logger.Warn("event channel full, shell detached event dropped", "shell_id", shellID)
		}
	}

	logger.Info("shell detached to background", "shell_id", shellID, "command", command)

	return shellID, nil
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

// CancelShell cancels a running background shell. It hands the kill to the
// supervisor (WindStop -> SIGKILL); the shellJob's Run then records the
// Cancelled state and the supervisor delivers the completion notification, so
// the process is waited on in exactly one place.
func (s *BackgroundShellService) CancelShell(shellID string) error {
	shell := s.shellTracker.Get(shellID)
	if shell == nil {
		return fmt.Errorf("shell not found: %s", shellID)
	}
	if shell.State != domain.ShellStateRunning {
		return fmt.Errorf("shell is not running (state: %s)", shell.State)
	}

	logger.Info("cancelling background shell", "shell_id", shellID)
	return s.supervisor.Wind(shellID, domain.WindStop)
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

// Stop is retained for the lifecycle contract. The supervisor owns the monitor
// goroutines and the cleanup ticker now, so there is nothing to stop here.
func (s *BackgroundShellService) Stop() {}

// generateShellID generates a unique shell ID
func generateShellID() string {
	return "shell-" + uuid.New().String()[:8]
}
