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
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

const (
	// InferNetworkPrefix is the prefix for session-specific Docker networks
	InferNetworkPrefix = "infer-network"
)

// GatewayManager manages the lifecycle of the gateway container or binary
type GatewayManager struct {
	sessionID        domain.SessionID
	config           *config.Config
	containerRuntime domain.ContainerRuntime
	containerID      string
	isRunning        bool
	binaryCmd        *exec.Cmd
	assignedPort     int
}

// NewGatewayManager creates a new gateway manager
func NewGatewayManager(sessionID domain.SessionID, cfg *config.Config, runtime domain.ContainerRuntime) *GatewayManager {
	return &GatewayManager{
		sessionID:        sessionID,
		config:           cfg,
		containerRuntime: runtime,
	}
}

// Start starts the gateway container or binary if configured to run locally
func (gm *GatewayManager) Start(ctx context.Context) error {
	if !gm.config.Gateway.Run {
		return nil
	}

	if gm.config.Gateway.StandaloneBinary {
		return gm.startBinary(ctx)
	}

	if gm.containerRuntime != nil && gm.config.Gateway.OCI != "" {
		return gm.startContainer(ctx)
	}

	return gm.startBinary(ctx)
}

// EnsureStarted starts the gateway if configured and not already running
// This is a convenience method that checks config and running state before starting
func (gm *GatewayManager) EnsureStarted() error {
	if !gm.config.Gateway.Run {
		return nil
	}

	if gm.isRunning {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := gm.Start(ctx); err != nil {
		return fmt.Errorf("failed to start gateway: %w", err)
	}

	return nil
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

	if gm.config.Gateway.Debug {
		fmt.Println("• Debug mode enabled - Gateway is running in development mode with detailed logging")
	}

	fmt.Println("• Starting gateway binary...")

	if err := gm.runBinary(binaryPath); err != nil {
		return fmt.Errorf("failed to start gateway binary: %w", err)
	}

	fmt.Println("• Waiting for gateway to become ready...")

	if err := gm.waitForReady(ctx); err != nil {
		if stopErr := gm.Stop(ctx); stopErr != nil {
			logger.Warn("Failed to stop gateway during error cleanup", "error", stopErr)
		}
		return fmt.Errorf("gateway failed to become ready: %w", err)
	}

	gm.isRunning = true
	fmt.Printf("• Gateway is ready at %s\n\n", gm.config.Gateway.URL)
	logger.Info("Gateway binary started successfully", "url", gm.config.Gateway.URL)
	return nil
}

// startContainer starts the gateway in a container
func (gm *GatewayManager) startContainer(ctx context.Context) error {
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

	if gm.containerRuntime != nil {
		if err := gm.containerRuntime.EnsureNetwork(ctx); err != nil {
			logger.Warn("Failed to create Docker network", "session", gm.sessionID, "error", err)
		}
	}

	if err := gm.pullImage(ctx); err != nil {
		logger.Warn("Failed to pull image, attempting to use local image", "error", err)
		fmt.Println("• Could not pull latest image, using cached version")
	}

	if gm.config.Gateway.Debug {
		fmt.Println("• Debug mode enabled - Gateway is running in development mode with detailed logging")
	}

	fmt.Println("• Starting gateway container...")

	if err := gm.runContainer(ctx); err != nil {
		return fmt.Errorf("failed to start gateway container: %w", err)
	}

	fmt.Println("• Waiting for gateway to become ready...")

	if err := gm.waitForReady(ctx); err != nil {
		if stopErr := gm.Stop(ctx); stopErr != nil {
			logger.Warn("Failed to stop gateway during error cleanup", "error", stopErr)
		}
		return fmt.Errorf("gateway failed to become ready: %w", err)
	}

	gm.isRunning = true
	actualURL := gm.GetGatewayURL()
	fmt.Printf("• Gateway is ready at %s\n\n", actualURL)
	logger.Info("Gateway container started successfully", "session", gm.sessionID, "url", actualURL, "port", gm.assignedPort)
	return nil
}

// Stop stops the gateway container or binary
func (gm *GatewayManager) Stop(ctx context.Context) error {
	if !gm.isRunning {
		return nil
	}

	if gm.containerRuntime != nil && gm.containerID != "" {
		return gm.stopContainer(ctx)
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

// stopContainer stops the container and cleans up the network
func (gm *GatewayManager) stopContainer(ctx context.Context) error {
	if gm.containerID == "" {
		return nil
	}

	if gm.containerRuntime != nil && !gm.containerRuntime.ContainerExists(gm.containerID) {
		gm.isRunning = false
		gm.containerID = ""
		if err := gm.containerRuntime.CleanupNetwork(ctx); err != nil {
			logger.Warn("Failed to cleanup network", "session", gm.sessionID, "error", err)
		}
		return nil
	}

	if gm.containerRuntime != nil {
		if err := gm.containerRuntime.StopContainer(ctx, gm.containerID); err != nil {
			logger.Warn("Failed to stop container", "session", gm.sessionID, "error", err)
		}
	}

	gm.isRunning = false
	gm.containerID = ""
	if gm.containerRuntime != nil {
		if err := gm.containerRuntime.CleanupNetwork(ctx); err != nil {
			logger.Warn("Failed to cleanup network", "session", gm.sessionID, "error", err)
		}
	}
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

// runContainer runs the gateway container using docker run command
func (gm *GatewayManager) runContainer(ctx context.Context) error {
	assignedPort := gm.determineGatewayPort()
	containerPort := "8080"

	containerName := fmt.Sprintf("inference-gateway-%s", gm.sessionID)
	var networkName string
	if gm.containerRuntime != nil {
		networkName = gm.containerRuntime.GetNetworkName()
	}
	args := []string{
		"run",
		"-d",
		"--name", containerName,
		"--network", networkName,
		"-p", fmt.Sprintf("%d:%s", assignedPort, containerPort),
		"--rm",
	}

	if _, err := os.Stat(".env"); err == nil {
		args = append(args, "--env-file", ".env")
	}

	apiKeyEnvVars := []string{
		"ANTHROPIC_API_KEY",
		"OPENAI_API_KEY",
		"GOOGLE_API_KEY",
		"DEEPSEEK_API_KEY",
		"GROQ_API_KEY",
		"MISTRAL_API_KEY",
		"CLOUDFLARE_API_KEY",
		"COHERE_API_KEY",
		"OLLAMA_API_KEY",
		"OLLAMA_CLOUD_API_KEY",
	}

	for _, envVar := range apiKeyEnvVars {
		if value := os.Getenv(envVar); value != "" {
			args = append(args, "-e", fmt.Sprintf("%s=%s", envVar, value))
		}
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

	timeout := gm.config.Gateway.Timeout
	if timeout > 0 {
		args = append(args, "-e", fmt.Sprintf("SERVER_READ_TIMEOUT=%ds", timeout))
		args = append(args, "-e", fmt.Sprintf("SERVER_WRITE_TIMEOUT=%ds", timeout))
		args = append(args, "-e", fmt.Sprintf("SERVER_IDLE_TIMEOUT=%ds", timeout))
		args = append(args, "-e", fmt.Sprintf("CLIENT_TIMEOUT=%ds", timeout))
	}

	if gm.config.Gateway.VisionEnabled {
		args = append(args, "-e", "ENABLE_VISION=true")
	}

	if gm.config.Gateway.Debug {
		args = append(args, "-e", "ENVIRONMENT=development")
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
	expectedName := fmt.Sprintf("inference-gateway-%s", gm.sessionID)
	cmd := exec.Command("docker", "ps", "--filter", "name=inference-gateway", "--format", "{{.ID}}\t{{.Names}}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 2 {
			continue
		}
		containerID := parts[0]
		foundName := parts[1]

		if foundName == expectedName {
			gm.containerID = containerID
			return true
		}
	}
	return false
}

// waitForReady waits for the gateway to become ready
func (gm *GatewayManager) waitForReady(ctx context.Context) error {
	actualURL := gm.GetGatewayURL()
	healthURL := strings.TrimSuffix(actualURL, "/") + "/health"

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
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("installer failed: %w", err)
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

	timeout := gm.config.Gateway.Timeout
	if timeout > 0 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("SERVER_READ_TIMEOUT=%ds", timeout))
		cmd.Env = append(cmd.Env, fmt.Sprintf("SERVER_WRITE_TIMEOUT=%ds", timeout))
		cmd.Env = append(cmd.Env, fmt.Sprintf("SERVER_IDLE_TIMEOUT=%ds", timeout))
		cmd.Env = append(cmd.Env, fmt.Sprintf("CLIENT_TIMEOUT=%ds", timeout))
	}

	if gm.config.Gateway.VisionEnabled {
		cmd.Env = append(cmd.Env, "ENABLE_VISION=true")
	}

	if gm.config.Gateway.Debug {
		cmd.Env = append(cmd.Env, "ENVIRONMENT=development")
	}

	// Configure gateway output streams
	if err := gm.configureGatewayOutput(cmd); err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start binary: %w", err)
	}

	gm.binaryCmd = cmd

	return nil
}

// configureGatewayOutput sets up stdout/stderr redirection for the gateway binary
func (gm *GatewayManager) configureGatewayOutput(cmd *exec.Cmd) error {
	if gm.config.Logging.ConsoleOutput == "stderr" {
		devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return fmt.Errorf("failed to open /dev/null: %w", err)
		}
		cmd.Stdout = devNull
		cmd.Stderr = devNull
		return nil
	}

	logDir := filepath.Join(".infer", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create gateway log directory: %w", err)
	}

	logFileName := fmt.Sprintf("gateway-%s.log", time.Now().Format("2006-01-02"))
	gatewayLogPath := filepath.Join(logDir, logFileName)

	logFile, err := os.OpenFile(gatewayLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open gateway log file: %w", err)
	}

	cmd.Stdout = logFile
	cmd.Stderr = logFile
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

// determineGatewayPort determines the port to use for the gateway
// If a port is already assigned, it returns that; otherwise finds an available port
func (gm *GatewayManager) determineGatewayPort() int {
	if gm.assignedPort > 0 {
		return gm.assignedPort
	}

	basePort := gm.extractPortFromURL()
	if basePort <= 0 {
		basePort = 8080
	}

	gm.assignedPort = config.FindAvailablePort(basePort)
	logger.Info("Assigned gateway port", "session", gm.sessionID, "port", gm.assignedPort)
	return gm.assignedPort
}

// extractPortFromURL extracts the port number from the configured gateway URL
func (gm *GatewayManager) extractPortFromURL() int {
	if !strings.Contains(gm.config.Gateway.URL, ":") {
		return 8080
	}

	parts := strings.Split(gm.config.Gateway.URL, ":")
	if len(parts) == 0 {
		return 8080
	}

	portStr := strings.TrimPrefix(parts[len(parts)-1], "/")
	portStr = strings.Split(portStr, "/")[0]

	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		return 8080
	}

	return port
}

// GetGatewayURL returns the actual gateway URL with the assigned port
func (gm *GatewayManager) GetGatewayURL() string {
	if gm.assignedPort == 0 {
		return gm.config.Gateway.URL
	}

	configURL := gm.config.Gateway.URL

	if !strings.Contains(configURL, "://") {
		return fmt.Sprintf("http://%s:%d", configURL, gm.assignedPort)
	}

	parts := strings.SplitN(configURL, "://", 2)
	if len(parts) != 2 {
		return fmt.Sprintf("http://localhost:%d", gm.assignedPort)
	}

	scheme := parts[0]
	rest := parts[1]

	hostAndPath := strings.SplitN(rest, "/", 2)
	host := hostAndPath[0]

	if strings.Contains(host, ":") {
		hostParts := strings.Split(host, ":")
		host = hostParts[0]
	}

	if len(hostAndPath) == 2 {
		return fmt.Sprintf("%s://%s:%d/%s", scheme, host, gm.assignedPort, hostAndPath[1])
	}

	return fmt.Sprintf("%s://%s:%d", scheme, host, gm.assignedPort)
}
