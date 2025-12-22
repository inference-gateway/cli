package services

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// UIManager manages the lifecycle of the web UI server
type UIManager struct {
	config    *config.Config
	cmd       *exec.Cmd
	isRunning bool
}

// NewUIManager creates a new UI manager
func NewUIManager(cfg *config.Config) *UIManager {
	return &UIManager{
		config: cfg,
	}
}

// Start starts the UI server process
func (um *UIManager) Start(ctx context.Context) error {
	if um.isRunning {
		return nil
	}

	switch um.config.API.UI.Mode {
	case "npm":
		return um.startNPM(ctx)
	case "docker":
		return fmt.Errorf("docker mode not yet implemented")
	default:
		return fmt.Errorf("unsupported UI mode: %s", um.config.API.UI.Mode)
	}
}

// startNPM starts the UI using npm dev server
func (um *UIManager) startNPM(ctx context.Context) error {
	logger.Info("Starting UI development server")

	workingDir := um.config.API.UI.WorkingDir
	if !filepath.IsAbs(workingDir) {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
		workingDir = filepath.Join(cwd, workingDir)
	}

	if _, err := os.Stat(workingDir); os.IsNotExist(err) {
		return fmt.Errorf("UI directory not found: %s", workingDir)
	}

	packageJSON := filepath.Join(workingDir, "package.json")
	if _, err := os.Stat(packageJSON); os.IsNotExist(err) {
		return fmt.Errorf("package.json not found in UI directory: %s", workingDir)
	}

	nodeModules := filepath.Join(workingDir, "node_modules")
	if _, err := os.Stat(nodeModules); os.IsNotExist(err) {
		logger.Info("Installing UI dependencies")
		fmt.Println("Installing UI dependencies (this may take a moment)...")
		installCmd := exec.Command("npm", "install")
		installCmd.Dir = workingDir
		installCmd.Stdout = os.Stdout
		installCmd.Stderr = os.Stderr
		if err := installCmd.Run(); err != nil {
			return fmt.Errorf("failed to install UI dependencies: %w", err)
		}
	}

	fmt.Println("Starting UI development server...")

	um.cmd = exec.Command("npm", "run", "dev")
	um.cmd.Dir = workingDir

	apiURL := fmt.Sprintf("http://localhost:%d", um.config.API.Port)
	um.cmd.Env = append(os.Environ(),
		fmt.Sprintf("PORT=%d", um.config.API.UI.Port),
		fmt.Sprintf("NEXT_PUBLIC_API_URL=%s", apiURL),
	)

	logger.Info("UI environment configured",
		"ui_port", um.config.API.UI.Port,
		"api_url", apiURL,
	)

	if um.config.Gateway.Debug {
		um.cmd.Stdout = os.Stdout
		um.cmd.Stderr = os.Stderr
	}

	if err := um.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start UI server: %w", err)
	}

	fmt.Println("Waiting for UI server to become ready...")

	if err := um.waitForReady(ctx); err != nil {
		if stopErr := um.Stop(); stopErr != nil {
			logger.Warn("Failed to stop UI server during error cleanup", "error", stopErr)
		}
		return fmt.Errorf("UI server failed to become ready: %w", err)
	}

	um.isRunning = true
	fmt.Printf("UI server is ready at %s\n\n", um.GetURL())
	logger.Info("UI server started successfully", "port", um.config.API.UI.Port)
	return nil
}

// waitForReady waits for the UI server to become ready by polling the root URL
func (um *UIManager) waitForReady(ctx context.Context) error {
	url := fmt.Sprintf("http://%s:%d", um.config.API.Host, um.config.API.UI.Port)

	timeout := 60 * time.Second
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for UI server to become ready")
			}

			resp, err := client.Get(url)
			if err == nil {
				if closeErr := resp.Body.Close(); closeErr != nil {
					logger.Warn("Failed to close response body", "error", closeErr)
				}
				if resp.StatusCode < 500 {
					return nil
				}
			}
		}
	}
}

// Stop stops the UI server process
func (um *UIManager) Stop() error {
	if !um.isRunning {
		return nil
	}

	if um.cmd == nil || um.cmd.Process == nil {
		return nil
	}

	logger.Info("Stopping UI server", "pid", um.cmd.Process.Pid)

	if err := um.cmd.Process.Kill(); err != nil {
		logger.Warn("Failed to kill UI server process", "error", err)
		return err
	}

	um.isRunning = false
	logger.Info("UI server stopped successfully")
	return nil
}

// IsRunning returns whether the UI server is running
func (um *UIManager) IsRunning() bool {
	return um.isRunning
}

// GetURL returns the URL where the UI is accessible
func (um *UIManager) GetURL() string {
	return fmt.Sprintf("http://localhost:%d", um.config.API.UI.Port)
}
