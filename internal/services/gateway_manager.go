package services

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// GatewayManager manages the lifecycle of the gateway container or binary
type GatewayManager struct {
	config      *config.Config
	containerID string
	isRunning   bool
	binaryCmd   *exec.Cmd
}

// NewGatewayManager creates a new gateway manager
func NewGatewayManager(cfg *config.Config) *GatewayManager {
	return &GatewayManager{
		config: cfg,
	}
}

// Start starts the gateway container or binary if configured to run locally
func (gm *GatewayManager) Start(ctx context.Context) error {
	if !gm.config.Gateway.Run {
		return nil
	}

	if gm.config.Gateway.Docker {
		return gm.startDocker(ctx)
	}

	return gm.startBinary(ctx)
}

// startBinary downloads and runs the gateway as a binary
func (gm *GatewayManager) startBinary(ctx context.Context) error {
	logger.Info("Starting gateway from binary")

	if gm.isBinaryRunning() {
		logger.Info("Gateway is already running on port")
		fmt.Println("• Gateway is already running")
		gm.isRunning = true
		return nil
	}

	binaryPath, err := gm.downloadBinary(ctx)
	if err != nil {
		return fmt.Errorf("failed to download gateway binary: %w", err)
	}

	fmt.Println("• Starting gateway binary...")

	if err := gm.runBinary(binaryPath); err != nil {
		return fmt.Errorf("failed to start gateway binary: %w", err)
	}

	fmt.Println("• Waiting for gateway to become ready...")

	if err := gm.waitForReady(ctx); err != nil {
		_ = gm.Stop(ctx)
		return fmt.Errorf("gateway failed to become ready: %w", err)
	}

	gm.isRunning = true
	fmt.Printf("• Gateway is ready at %s\n\n", gm.config.Gateway.URL)
	logger.Info("Gateway binary started successfully", "url", gm.config.Gateway.URL)
	return nil
}

// startDocker starts the gateway in a Docker container
func (gm *GatewayManager) startDocker(ctx context.Context) error {
	if gm.config.Gateway.OCI == "" {
		return fmt.Errorf("gateway OCI image not specified in configuration")
	}

	logger.Info("Starting gateway container", "image", gm.config.Gateway.OCI)

	if gm.isContainerRunning() {
		logger.Info("Gateway container is already running")
		fmt.Println("• Gateway container is already running")
		gm.isRunning = true
		return nil
	}

	if err := gm.pullImage(ctx); err != nil {
		logger.Warn("Failed to pull image, attempting to use local image", "error", err)
		fmt.Println("• Could not pull latest image, using cached version")
	}

	fmt.Println("• Starting gateway container...")

	if err := gm.startContainer(ctx); err != nil {
		return fmt.Errorf("failed to start gateway container: %w", err)
	}

	fmt.Println("• Waiting for gateway to become ready...")

	if err := gm.waitForReady(ctx); err != nil {
		_ = gm.Stop(ctx)
		return fmt.Errorf("gateway failed to become ready: %w", err)
	}

	gm.isRunning = true
	fmt.Printf("• Gateway is ready at %s\n\n", gm.config.Gateway.URL)
	logger.Info("Gateway container started successfully", "url", gm.config.Gateway.URL)
	return nil
}

// Stop stops the gateway container or binary
func (gm *GatewayManager) Stop(ctx context.Context) error {
	if !gm.isRunning {
		return nil
	}

	if gm.config.Gateway.Docker {
		return gm.stopDocker(ctx)
	}

	return gm.stopBinary()
}

// stopBinary stops the binary process
func (gm *GatewayManager) stopBinary() error {
	if gm.binaryCmd == nil || gm.binaryCmd.Process == nil {
		return nil
	}

	logger.Info("Stopping gateway binary", "pid", gm.binaryCmd.Process.Pid)

	if err := gm.binaryCmd.Process.Kill(); err != nil {
		logger.Warn("Failed to kill binary process", "error", err)
		return err
	}

	gm.isRunning = false
	gm.binaryCmd = nil
	logger.Info("Gateway binary stopped")
	return nil
}

// stopDocker stops the Docker container
func (gm *GatewayManager) stopDocker(ctx context.Context) error {
	if gm.containerID == "" {
		return nil
	}

	logger.Info("Stopping gateway container", "containerID", gm.containerID)

	cmd := exec.CommandContext(ctx, "docker", "stop", gm.containerID)
	if err := cmd.Run(); err != nil {
		logger.Warn("Failed to stop container", "error", err)
	}

	cmd = exec.CommandContext(ctx, "docker", "rm", gm.containerID)
	if err := cmd.Run(); err != nil {
		logger.Warn("Failed to remove container", "error", err)
	}

	gm.isRunning = false
	gm.containerID = ""
	logger.Info("Gateway container stopped")
	return nil
}

// IsRunning returns whether the gateway container is running
func (gm *GatewayManager) IsRunning() bool {
	return gm.isRunning
}

// pullImage pulls the OCI image with progress feedback
func (gm *GatewayManager) pullImage(ctx context.Context) error {
	fmt.Printf("• Pulling gateway image: %s\n", gm.config.Gateway.OCI)

	cmd := exec.CommandContext(ctx, "docker", "pull", gm.config.Gateway.OCI)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker pull failed: %w, output: %s", err, string(output))
	}

	fmt.Println("• Gateway image pulled successfully")
	return nil
}

// startContainer starts the gateway container
func (gm *GatewayManager) startContainer(ctx context.Context) error {
	port := "8080"
	if strings.Contains(gm.config.Gateway.URL, ":") {
		parts := strings.Split(gm.config.Gateway.URL, ":")
		if len(parts) > 0 {
			port = strings.TrimPrefix(parts[len(parts)-1], "/")
		}
	}

	args := []string{
		"run",
		"-d",
		"--name", "inference-gateway",
		"-p", fmt.Sprintf("%s:%s", port, port),
		"--rm",
	}

	if _, err := os.Stat(".env"); err == nil {
		args = append(args, "--env-file", ".env")
	}

	if gm.config.Gateway.APIKey != "" {
		args = append(args, "-e", fmt.Sprintf("API_KEY=%s", gm.config.Gateway.APIKey))
	}

	if len(gm.config.Gateway.IncludeModels) > 0 {
		includeModels := strings.Join(gm.config.Gateway.IncludeModels, ",")
		args = append(args, "-e", fmt.Sprintf("ALLOWED_MODELS=%s", includeModels))
	}

	if len(gm.config.Gateway.ExcludeModels) > 0 {
		excludeModels := strings.Join(gm.config.Gateway.ExcludeModels, ",")
		args = append(args, "-e", fmt.Sprintf("DISALLOWED_MODELS=%s", excludeModels))
	}

	args = append(args, gm.config.Gateway.OCI)

	logger.Info("Starting gateway container", "command", fmt.Sprintf("docker %s", strings.Join(args, " ")))
	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker run failed: %w, output: %s", err, string(output))
	}

	gm.containerID = strings.TrimSpace(string(output))
	return nil
}

// isContainerRunning checks if a gateway container is already running
func (gm *GatewayManager) isContainerRunning() bool {
	cmd := exec.Command("docker", "ps", "--filter", "name=inference-gateway", "--format", "{{.ID}}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}

	containerID := strings.TrimSpace(string(output))
	if containerID != "" {
		gm.containerID = containerID
		return true
	}
	return false
}

// waitForReady waits for the gateway to become ready
func (gm *GatewayManager) waitForReady(ctx context.Context) error {
	healthURL := strings.TrimSuffix(gm.config.Gateway.URL, "/") + "/health"

	timeout := time.Duration(gm.config.Gateway.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

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
				return fmt.Errorf("timeout waiting for gateway to become ready")
			}

			resp, err := client.Get(healthURL)
			if err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return nil
				}
			}
		}
	}
}

// isBinaryRunning checks if the gateway is already running on the port
func (gm *GatewayManager) isBinaryRunning() bool {
	healthURL := strings.TrimSuffix(gm.config.Gateway.URL, "/") + "/health"
	client := &http.Client{Timeout: 1 * time.Second}
	resp, err := client.Get(healthURL)
	if err == nil {
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}
	return false
}

// downloadBinary downloads the latest gateway binary using the installer script
func (gm *GatewayManager) downloadBinary(ctx context.Context) (string, error) {
	binaryDir := filepath.Join(".infer", "bin")
	if err := os.MkdirAll(binaryDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create binary directory: %w", err)
	}

	binaryPath := filepath.Join(binaryDir, "inference-gateway")

	if _, err := os.Stat(binaryPath); err == nil {
		return binaryPath, nil
	}

	logger.Info("Downloading latest gateway binary using installer")

	fmt.Println("• Downloading gateway binary...")

	absBinaryDir, err := filepath.Abs(binaryDir)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	installCmd := fmt.Sprintf("curl -fsSL https://raw.githubusercontent.com/inference-gateway/inference-gateway/main/install.sh | INSTALL_DIR=%s bash", absBinaryDir)

	cmd := exec.CommandContext(ctx, "bash", "-c", installCmd)
	cmd.Stdin = nil

	output, err := cmd.CombinedOutput()

	if err != nil {
		return "", fmt.Errorf("installer failed: %w, output: %s", err, string(output))
	}

	if _, err := os.Stat(binaryPath); err != nil {
		return "", fmt.Errorf("binary not found after installation: %w", err)
	}

	fmt.Println("• Gateway binary downloaded successfully")
	logger.Info("Gateway binary installed successfully", "path", binaryPath)
	return binaryPath, nil
}

// runBinary starts the gateway binary
func (gm *GatewayManager) runBinary(binaryPath string) error {
	cmd := exec.Command(binaryPath)
	cmd.Env = gm.loadEnvironment()

	if gm.config.Gateway.APIKey != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("API_KEY=%s", gm.config.Gateway.APIKey))
	}

	if len(gm.config.Gateway.IncludeModels) > 0 {
		includeModels := strings.Join(gm.config.Gateway.IncludeModels, ",")
		cmd.Env = append(cmd.Env, fmt.Sprintf("ALLOWED_MODELS=%s", includeModels))
	}

	if len(gm.config.Gateway.ExcludeModels) > 0 {
		excludeModels := strings.Join(gm.config.Gateway.ExcludeModels, ",")
		cmd.Env = append(cmd.Env, fmt.Sprintf("DISALLOWED_MODELS=%s", excludeModels))
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start binary: %w", err)
	}

	gm.binaryCmd = cmd

	return nil
}

// loadEnvironment loads environment variables from .env file or system environment
func (gm *GatewayManager) loadEnvironment() []string {
	if _, err := os.Stat(".env"); err != nil {
		return os.Environ()
	}

	envVars := os.Environ()
	envFile, err := os.ReadFile(".env")
	if err != nil {
		return envVars
	}

	lines := strings.Split(string(envFile), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "=") {
			envVars = append(envVars, line)
		}
	}

	return envVars
}
