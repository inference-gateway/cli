package gateway

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	containerruntime "github.com/inference-gateway/cli/internal/platform/container"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
)

// Manager manages the lifecycle of the gateway container or binary
type Manager struct {
	sessionID        convdomain.SessionID
	config           *config.Config
	containerRuntime containerruntime.ContainerRuntime
	containerID      string
	isRunning        bool
	binaryCmd        *exec.Cmd
	assignedPort     int
}

// NewManager creates a new gateway manager
func NewManager(sessionID convdomain.SessionID, cfg *config.Config, runtime containerruntime.ContainerRuntime) *Manager {
	return &Manager{
		sessionID:        sessionID,
		config:           cfg,
		containerRuntime: runtime,
	}
}

// Start starts the gateway container or binary if configured to run locally
func (gm *Manager) Start(ctx context.Context) error {
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
func (gm *Manager) EnsureStarted() error {
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
func (gm *Manager) startBinary(ctx context.Context) error {
	logger.Info("starting gateway from binary")

	if gm.isBinaryRunning() {
		if !gm.needsAudioRestart() {
			logger.Info("gateway is already running on port")
			fmt.Printf("• Gateway%s is already running\n", gm.versionLabel(ctx))
			gm.isRunning = true
			gm.registerPID()
			logger.Debug("registered PID on existing gateway")
			return nil
		}
		logger.Info("running gateway lacks the Audio API, restarting with AUDIO_ENABLED=true")
		fmt.Println("• Restarting gateway to enable the Audio API...")
		gm.killGateway()
		gm.removeGatewayPID()
		gm.waitForStopped(ctx)
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
		if gm.binaryCmd != nil && gm.binaryCmd.Process != nil {
			if killErr := gm.binaryCmd.Process.Kill(); killErr != nil {
				logger.Warn("failed to kill gateway binary during error cleanup", "error", killErr)
			}
			_ = gm.binaryCmd.Wait()
			gm.binaryCmd = nil
		}
		return fmt.Errorf("gateway failed to become ready: %w", err)
	}

	gm.isRunning = true
	gm.registerPID()
	gm.writeGatewayPID()
	fmt.Printf("• Gateway%s is ready at %s\n\n", gm.versionLabel(ctx), gm.config.Gateway.URL)
	logger.Info("gateway binary started successfully", "url", gm.config.Gateway.URL)
	return nil
}

// startContainer starts the gateway in a container
func (gm *Manager) startContainer(ctx context.Context) error {
	if gm.config.Gateway.OCI == "" {
		return fmt.Errorf("gateway OCI image not specified in configuration")
	}

	logger.Info("starting gateway container", "image", gm.config.Gateway.OCI)

	if gm.isContainerRunning() {
		if !gm.needsAudioRestart() {
			logger.Info("gateway container is already running")
			fmt.Printf("• Gateway container%s is already running\n", gm.versionLabel(ctx))
			gm.isRunning = true
			return nil
		}
		logger.Info("running gateway container lacks the Audio API, restarting with AUDIO_ENABLED=true")
		fmt.Println("• Restarting gateway container to enable the Audio API...")
		if err := gm.stopContainer(ctx); err != nil {
			logger.Warn("failed to stop gateway container for audio restart", "error", err)
		}
	}

	if gm.containerRuntime != nil {
		if err := gm.containerRuntime.EnsureNetwork(ctx); err != nil {
			logger.Warn("failed to create Docker network", "session", gm.sessionID, "error", err)
		}
	}

	if err := gm.pullImage(ctx); err != nil {
		logger.Warn("failed to pull image, attempting to use local image", "error", err)
		fmt.Println("• Could not pull latest image, using cached version")
	}

	if gm.config.Gateway.Debug {
		fmt.Println("• Debug mode enabled - Gateway is running in development mode with detailed logging")
	}

	fmt.Println("• Starting gateway container...")

	if err := gm.runContainer(ctx); err != nil {
		return fmt.Errorf("failed to start gateway container: %w", err)
	}

	gm.isRunning = true

	fmt.Println("• Waiting for gateway to become ready...")

	if err := gm.waitForReady(ctx); err != nil {
		if stopErr := gm.Stop(ctx); stopErr != nil {
			logger.Warn("failed to stop gateway during error cleanup", "error", stopErr)
		}
		return fmt.Errorf("gateway failed to become ready: %w", err)
	}

	actualURL := gm.GetGatewayURL()
	fmt.Printf("• Gateway%s is ready at %s\n\n", gm.versionLabel(ctx), actualURL)
	logger.Info("gateway container started successfully", "session", gm.sessionID, "url", actualURL, "port", gm.assignedPort)
	return nil
}

// Stop stops the gateway container or binary and cleans up the network
func (gm *Manager) Stop(ctx context.Context) error {
	if !gm.isRunning {
		return nil
	}

	var stopErr error
	if gm.containerRuntime != nil && gm.containerID != "" {
		stopErr = gm.stopContainer(ctx)
	} else {
		stopErr = gm.stopBinary()
	}

	if gm.containerRuntime != nil {
		if err := gm.containerRuntime.CleanupNetwork(ctx); err != nil {
			logger.Warn("failed to cleanup network during gateway shutdown", "session", gm.sessionID, "error", err)
		}
	}

	return stopErr
}

// stopBinary stops the binary process using PID-file reference counting.
// Every process that started or attached to the shared binary registers
// its PID in ~/.infer/run/pids/. On stop, it deregisters itself and
// prunes stale entries (crashed processes). The binary is killed only
// when the last live registration is gone - whichever process exits last
// turns off the lights.
func (gm *Manager) stopBinary() error {
	gm.deregisterPID()

	if gm.pruneAndCheckLive() {
		logger.Info("other gateway users still active, deferring binary shutdown")
	} else {
		gm.killGateway()
		gm.removeGatewayPID()
		logger.Info("gateway binary stopped")
	}
	gm.isRunning = false
	gm.binaryCmd = nil
	return nil
}

// killGateway kills the shared gateway process, using the saved gateway
// PID file or the in-process binaryCmd handle as fallback.
func (gm *Manager) killGateway() {
	if gm.binaryCmd != nil && gm.binaryCmd.Process != nil {
		logger.Info("last process stopping gateway binary", "pid", gm.binaryCmd.Process.Pid)
		if err := gm.binaryCmd.Process.Kill(); err != nil {
			logger.Warn("failed to kill gateway binary", "error", err)
		}
		return
	}

	if pid := gm.readGatewayPID(); pid > 0 && utils.ProcessAlive(pid) {
		logger.Info("last process stopping gateway binary", "pid", pid)
		proc, err := os.FindProcess(pid)
		if err == nil {
			if err := proc.Kill(); err != nil {
				logger.Warn("failed to kill gateway binary", "pid", pid, "error", err)
			}
		}
	}
}

// inferHomeDir returns a path under the userspace ~/.infer directory.
// $HOME is always set on supported platforms; no project-relative fallback.
func inferHomeDir(part string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, config.ConfigDirName, part)
}

// inferRunDir returns the runtime state directory (~/.infer/run).
func (gm *Manager) inferRunDir() string {
	return inferHomeDir("run")
}

// pidsDir returns the consumer PID registry directory (~/.infer/run/pids).
func (gm *Manager) pidsDir() string {
	return filepath.Join(gm.inferRunDir(), "pids")
}

// gatewayPIDPath returns the gateway binary PID file path (~/.infer/run/gateway.pid).
func (gm *Manager) gatewayPIDPath() string {
	return filepath.Join(gm.inferRunDir(), "gateway.pid")
}

// registerPID drops a PID file for this consumer process.
func (gm *Manager) registerPID() {
	pidDir := gm.pidsDir()
	if err := os.MkdirAll(pidDir, 0755); err != nil {
		logger.Warn("failed to create PID directory", "error", err)
		return
	}
	pidPath := filepath.Join(pidDir, strconv.Itoa(os.Getpid()))
	if err := os.WriteFile(pidPath, nil, 0644); err != nil {
		logger.Warn("failed to register PID", "error", err)
	}
}

// deregisterPID removes this process's PID file.
func (gm *Manager) deregisterPID() {
	pidPath := filepath.Join(gm.pidsDir(), strconv.Itoa(os.Getpid()))
	if err := os.Remove(pidPath); err != nil && !os.IsNotExist(err) {
		logger.Warn("failed to deregister PID", "error", err)
	}
}

// pruneAndCheckLive prunes stale consumer PID files (dead processes) and
// reports whether any live registrations remain. Our own PID is already
// deregistered before this is called.
func (gm *Manager) pruneAndCheckLive() bool {
	pidDir := gm.pidsDir()
	entries, err := os.ReadDir(pidDir)
	if err != nil {
		return false
	}
	anyLive := false
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			_ = os.Remove(filepath.Join(pidDir, e.Name()))
			continue
		}
		if !utils.ProcessAlive(pid) {
			_ = os.Remove(filepath.Join(pidDir, e.Name()))
			continue
		}
		anyLive = true
	}
	return anyLive
}

// writeGatewayPID writes the spawned gateway binary's PID to the shared file.
func (gm *Manager) writeGatewayPID() {
	if gm.binaryCmd == nil || gm.binaryCmd.Process == nil {
		return
	}
	pidDir := filepath.Dir(gm.gatewayPIDPath())
	if err := os.MkdirAll(pidDir, 0755); err != nil {
		logger.Warn("failed to create gateway PID directory", "error", err)
		return
	}
	data := strconv.Itoa(gm.binaryCmd.Process.Pid)
	if err := os.WriteFile(gm.gatewayPIDPath(), []byte(data), 0644); err != nil {
		logger.Warn("failed to write gateway PID", "error", err)
	}
}

// readGatewayPID reads the gateway binary PID from the shared file, or 0.
func (gm *Manager) readGatewayPID() int {
	data, err := os.ReadFile(gm.gatewayPIDPath())
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}

// removeGatewayPID removes the gateway PID file.
func (gm *Manager) removeGatewayPID() {
	if err := os.Remove(gm.gatewayPIDPath()); err != nil && !os.IsNotExist(err) {
		logger.Warn("failed to remove gateway PID file", "error", err)
	}
}

// stopContainer stops the container (network cleanup is handled in Stop() method)
func (gm *Manager) stopContainer(ctx context.Context) error {
	if gm.containerID == "" {
		return nil
	}

	if gm.containerRuntime != nil && !gm.containerRuntime.ContainerExists(gm.containerID) {
		gm.isRunning = false
		gm.containerID = ""
		return nil
	}

	if gm.containerRuntime != nil {
		if err := gm.containerRuntime.StopContainer(ctx, gm.containerID); err != nil {
			logger.Warn("failed to stop container", "session", gm.sessionID, "error", err)
		}
	}

	gm.isRunning = false
	gm.containerID = ""
	return nil
}

// IsRunning returns whether the gateway container is running
func (gm *Manager) IsRunning() bool {
	return gm.isRunning
}

// pullImage pulls the OCI image with progress feedback
func (gm *Manager) pullImage(ctx context.Context) error {
	fmt.Printf("• Pulling gateway image: %s\n", gm.config.Gateway.OCI)

	cmd := exec.CommandContext(ctx, "docker", "pull", gm.config.Gateway.OCI)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker pull failed: %w", err)
	}

	fmt.Println("• Gateway image pulled successfully")
	return nil
}

// runContainer runs the gateway container using docker run command
func (gm *Manager) runContainer(ctx context.Context) error {
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
		args = append(args, "-e", fmt.Sprintf("CLIENT_RESPONSE_HEADER_TIMEOUT=%ds", timeout))
	}

	if gm.config.Gateway.VisionEnabled {
		args = append(args, "-e", "ENABLE_VISION=true")
	}

	if gm.config.Tools.ImageGeneration.Enabled || gm.config.Tools.ImageEdit.Enabled || gm.config.Tools.ImageVariation.Enabled {
		args = append(args, "-e", "ENABLE_IMAGES=true")
	}

	if gm.config.TextToSpeech.Enabled && gm.config.TextToSpeech.IsGatewayEngine() {
		args = append(args, "-e", "AUDIO_ENABLED=true")
		args = append(args, "-e", fmt.Sprintf("AUDIO_LOCAL_AUTO_DOWNLOAD=%t", gm.config.TextToSpeech.AutoDownload))
	}

	if gm.config.Gateway.Debug {
		args = append(args, "-e", "ENVIRONMENT=development")
	}

	args = append(args, gm.config.Gateway.OCI)

	logger.Info("starting gateway container", "command", fmt.Sprintf("docker %s", strings.Join(args, " ")))
	cmd := exec.CommandContext(ctx, "docker", args...)

	var outputBuf strings.Builder
	cmd.Stdout = &outputBuf
	cmd.Stderr = io.Discard

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker run failed: %w", err)
	}

	gm.containerID = strings.TrimSpace(outputBuf.String())
	return nil
}

// isContainerRunning checks if a gateway container is already running
func (gm *Manager) isContainerRunning() bool {
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
func (gm *Manager) waitForReady(ctx context.Context) error {
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

// needsAudioRestart reports whether an already-running gateway must be
// restarted because the configured text-to-speech engine needs its Audio API
// but the running instance was started without AUDIO_ENABLED.
func (gm *Manager) needsAudioRestart() bool {
	return gm.config.TextToSpeech.Enabled && gm.config.TextToSpeech.IsGatewayEngine() && !gm.audioAPIEnabled()
}

// audioAPIEnabled probes POST /v1/audio/speech on the running gateway. The
// route only exists when the gateway runs with AUDIO_ENABLED, so a 404 means
// audio is off; any other response (400, 401, 503, ...) means it is served.
func (gm *Manager) audioAPIEnabled() bool {
	url := strings.TrimSuffix(gm.config.Gateway.URL, "/") + "/v1/audio/speech"
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Post(url, "application/json", strings.NewReader("{}"))
	if err != nil {
		return true
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode != http.StatusNotFound
}

// waitForStopped waits briefly for a killed gateway to stop answering health
// checks so the replacement can bind the port.
func (gm *Manager) waitForStopped(ctx context.Context) {
	deadline := time.Now().Add(5 * time.Second)
	for gm.isBinaryRunning() && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// Version reports the version of the gateway serving at the managed URL, or ""
// when it cannot be determined. The gateway's /version endpoint wins; managed
// modes fall back to the cached binary or the configured container image tag.
// Externally managed gateways (Gateway.Run off) have no local source to ask.
func (gm *Manager) Version(ctx context.Context) string {
	if v := probeVersionEndpoint(ctx, gm.GetGatewayURL()); v != "" {
		return v
	}

	if !gm.config.Gateway.Run {
		return ""
	}

	if gm.config.Gateway.OCI != "" && !gm.config.Gateway.StandaloneBinary {
		return imageVersion(gm.config.Gateway.OCI)
	}

	return binaryVersion(ctx, gatewayBinaryPath())
}

// versionLabel renders " v0.50.0" for the startup lines, or "" when unknown.
func (gm *Manager) versionLabel(ctx context.Context) string {
	if v := gm.Version(ctx); v != "" {
		return " v" + v
	}
	return ""
}

// probeVersionEndpoint asks the gateway's /version endpoint, accepting a JSON
// payload with a "version" field or a plain-text body. Anything else (a 404
// on gateways without the endpoint, HTML, connection errors) yields "".
func probeVersionEndpoint(ctx context.Context, gatewayURL string) string {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, strings.TrimSuffix(gatewayURL, "/")+"/version", nil)
	if err != nil {
		return ""
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return ""
	}

	var payload struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Version != "" {
		return cleanVersion(payload.Version)
	}

	return cleanVersion(string(body))
}

// binaryVersion runs the managed gateway binary's --version, mirroring the
// parsing (and failure tolerance) of gatewayBinaryIsStale.
func binaryVersion(ctx context.Context, binaryPath string) string {
	if _, err := os.Stat(binaryPath); err != nil {
		return ""
	}

	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(checkCtx, binaryPath, "--version").Output()
	fields := strings.Fields(string(out))
	if err != nil || len(fields) == 0 {
		return ""
	}

	return cleanVersion(fields[len(fields)-1])
}

// imageVersion extracts the version from the configured container image tag,
// e.g. ghcr.io/inference-gateway/inference-gateway:v0.50.0 -> "0.50.0".
// Floating references (:latest, digests) say nothing and yield "".
func imageVersion(image string) string {
	tag := image[strings.LastIndex(image, "/")+1:]
	if idx := strings.Index(tag, ":"); idx >= 0 {
		tag = tag[idx+1:]
	}
	return cleanVersion(tag)
}

// cleanVersion normalizes a version ("v0.50.0" -> "0.50.0") and rejects values
// that do not look like one (HTML bodies, "latest", "sha256:...").
func cleanVersion(v string) string {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" || len(v) > 32 || v[0] < '0' || v[0] > '9' {
		return ""
	}
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '.', r == '-', r == '+':
		default:
			return ""
		}
	}
	return v
}

// gatewayBinaryPath returns where the managed gateway binary lives.
func gatewayBinaryPath() string {
	name := "inference-gateway"
	if runtime.GOOS == "windows" {
		name = "inference-gateway.exe"
	}
	return filepath.Join(inferHomeDir("bin"), name)
}

// isBinaryRunning checks if the gateway is already running on the port
func (gm *Manager) isBinaryRunning() bool {
	healthURL := strings.TrimSuffix(gm.config.Gateway.URL, "/") + "/health"
	client := &http.Client{Timeout: 1 * time.Second}
	resp, err := client.Get(healthURL)
	if err == nil {
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}
	return false
}

// downloadBinary downloads the latest gateway binary release directly from
// GitHub, authenticating the API call with GITHUB_TOKEN/GH_TOKEN when
// available to avoid the 60 req/hour unauthenticated rate limit
func (gm *Manager) downloadBinary(ctx context.Context) (string, error) {
	binaryPath := gatewayBinaryPath()
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0755); err != nil {
		return "", fmt.Errorf("failed to create binary directory: %w", err)
	}

	if _, err := os.Stat(binaryPath); err == nil {
		if !gatewayBinaryIsStale(ctx, binaryPath) {
			return binaryPath, nil
		}
		fmt.Println("• Updating stale gateway binary...")
	} else {
		fmt.Println("• Downloading gateway binary...")
	}

	logger.Info("downloading latest gateway binary")

	tag, err := latestGatewayTag(ctx)
	if err != nil {
		return "", err
	}

	assetOS, assetArch, err := gatewayAssetPlatform()
	if err != nil {
		return "", err
	}

	assetExt := "tar.gz"
	if runtime.GOOS == "windows" {
		assetExt = "zip"
	}
	assetURL := fmt.Sprintf(
		"https://github.com/inference-gateway/inference-gateway/releases/download/%s/inference-gateway_%s_%s.%s",
		tag, assetOS, assetArch, assetExt,
	)

	if err := downloadAndExtractGatewayBinary(ctx, assetURL, binaryPath); err != nil {
		return "", err
	}

	fmt.Println("• Gateway binary downloaded successfully")
	logger.Info("gateway binary installed successfully", "path", binaryPath, "version", tag)
	return binaryPath, nil
}

// gatewayBinaryIsStale reports whether the cached gateway binary is older than the
// latest release. Any failure (offline, rate limit, unparsable version output)
// reports false so startup never breaks on a GitHub hiccup — the cached binary
// keeps working as before.
func gatewayBinaryIsStale(ctx context.Context, binaryPath string) bool {
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(checkCtx, binaryPath, "--version").Output()
	fields := strings.Fields(string(out))
	if err != nil || len(fields) == 0 {
		return false
	}
	current := strings.TrimPrefix(fields[len(fields)-1], "v")

	tag, err := latestGatewayTag(checkCtx)
	if err != nil {
		return false
	}
	latest := strings.TrimPrefix(tag, "v")

	if current == "" || latest == "" || current == latest {
		return false
	}
	logger.Info("cached gateway binary is stale", "current", current, "latest", latest)
	return true
}

// githubToken returns the GitHub token from the environment, preferring
// GITHUB_TOKEN and falling back to GH_TOKEN (matching the gh CLI)
func githubToken() string {
	if t := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); t != "" {
		return t
	}
	return strings.TrimSpace(os.Getenv("GH_TOKEN"))
}

// latestGatewayTag resolves the latest gateway release tag via the GitHub
// API, sending an Authorization header when a token is available
func latestGatewayTag(ctx context.Context) (string, error) {
	apiURL := "https://api.github.com/repos/inference-gateway/inference-gateway/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create release request: %w", err)
	}
	if t := githubToken(); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to query latest gateway release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return "", fmt.Errorf("GitHub API rate limit exceeded (60 req/hour for unauthenticated requests) - set GITHUB_TOKEN (or GH_TOKEN) to raise the limit to 5,000/hour, or try again later")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to query latest gateway release: HTTP %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("failed to decode release response: %w", err)
	}
	if release.TagName == "" {
		return "", fmt.Errorf("latest gateway release has no tag name")
	}
	return release.TagName, nil
}

// gatewayAssetPlatform maps the current OS/arch to the gateway release asset
// naming scheme (inference-gateway_<Os>_<arch>.tar.gz)
func gatewayAssetPlatform() (string, string, error) {
	var assetOS string
	switch runtime.GOOS {
	case "darwin":
		assetOS = "Darwin"
	case "linux":
		assetOS = "Linux"
	case "windows":
		assetOS = "Windows"
	default:
		return "", "", fmt.Errorf("no gateway binary release for OS %q", runtime.GOOS)
	}

	var assetArch string
	switch runtime.GOARCH {
	case "amd64":
		assetArch = "x86_64"
	case "arm64":
		assetArch = "arm64"
	case "arm":
		assetArch = "armv7"
	default:
		return "", "", fmt.Errorf("no gateway binary release for architecture %q", runtime.GOARCH)
	}

	return assetOS, assetArch, nil
}

// downloadAndExtractGatewayBinary downloads a release archive and extracts
// the inference-gateway binary from it to destPath. Supports .tar.gz and .zip.
func downloadAndExtractGatewayBinary(ctx context.Context, url string, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create download request: %w", err)
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download gateway release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download gateway release from %s: HTTP %d", url, resp.StatusCode)
	}

	if strings.HasSuffix(url, ".zip") {
		return extractGatewayZip(resp.Body, destPath)
	}

	return extractGatewayTarGz(resp.Body, destPath)
}

// extractGatewayTarGz extracts the inference-gateway binary from a gzipped tarball
func extractGatewayTarGz(r io.Reader, destPath string) error {
	gzReader, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("failed to read gateway release archive: %w", err)
	}
	defer func() { _ = gzReader.Close() }()

	tarReader := tar.NewReader(gzReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read gateway release archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg || filepath.Base(header.Name) != "inference-gateway" {
			continue
		}

		out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			return fmt.Errorf("failed to create gateway binary: %w", err)
		}
		if _, err := io.Copy(out, tarReader); err != nil {
			_ = out.Close()
			_ = os.Remove(destPath)
			return fmt.Errorf("failed to write gateway binary: %w", err)
		}
		return out.Close()
	}

	return fmt.Errorf("inference-gateway binary not found in release archive")
}

// extractGatewayZip extracts the inference-gateway.exe binary from a zip archive
func extractGatewayZip(r io.Reader, destPath string) error {
	tmpFile, err := os.CreateTemp("", "infer-gateway-*.zip")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := io.Copy(tmpFile, r); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write temp archive: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp archive: %w", err)
	}

	zipReader, err := zip.OpenReader(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to read gateway release zip: %w", err)
	}
	defer func() { _ = zipReader.Close() }()

	for _, f := range zipReader.File {
		if filepath.Base(f.Name) != "inference-gateway.exe" {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("failed to open entry in zip: %w", err)
		}

		out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			_ = rc.Close()
			return fmt.Errorf("failed to create gateway binary: %w", err)
		}
		if _, err := io.Copy(out, rc); err != nil {
			_ = out.Close()
			_ = os.Remove(destPath)
			_ = rc.Close()
			return fmt.Errorf("failed to write gateway binary: %w", err)
		}
		if err := out.Close(); err != nil {
			_ = rc.Close()
			return err
		}
		_ = rc.Close()
		return nil
	}

	return fmt.Errorf("inference-gateway.exe binary not found in release archive")
}

// runBinary starts the gateway binary
func (gm *Manager) runBinary(binaryPath string) error {
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
		cmd.Env = append(cmd.Env, fmt.Sprintf("CLIENT_RESPONSE_HEADER_TIMEOUT=%ds", timeout))
	}

	if gm.config.Gateway.VisionEnabled {
		cmd.Env = append(cmd.Env, "ENABLE_VISION=true")
	}

	if gm.config.Tools.ImageGeneration.Enabled || gm.config.Tools.ImageEdit.Enabled || gm.config.Tools.ImageVariation.Enabled {
		cmd.Env = append(cmd.Env, "ENABLE_IMAGES=true")
	}

	if gm.config.TextToSpeech.Enabled && gm.config.TextToSpeech.IsGatewayEngine() {
		cmd.Env = append(cmd.Env, "AUDIO_ENABLED=true")
		cmd.Env = append(cmd.Env, fmt.Sprintf("AUDIO_LOCAL_AUTO_DOWNLOAD=%t", gm.config.TextToSpeech.AutoDownload))
	}

	if gm.config.Gateway.Debug {
		cmd.Env = append(cmd.Env, "ENVIRONMENT=development")
	}

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
func (gm *Manager) configureGatewayOutput(cmd *exec.Cmd) error {
	logDir := gm.config.Logging.Dir
	if logDir == "" {
		logDir = config.DefaultLogsDir()
	}
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
func (gm *Manager) loadEnvironment() []string {
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
func (gm *Manager) determineGatewayPort() int {
	if gm.assignedPort > 0 {
		return gm.assignedPort
	}

	basePort := gm.extractPortFromURL()
	if basePort <= 0 {
		basePort = 8080
	}

	gm.assignedPort = config.FindAvailablePort(basePort)
	logger.Info("assigned gateway port", "session", gm.sessionID, "port", gm.assignedPort)
	return gm.assignedPort
}

// extractPortFromURL extracts the port number from the configured gateway URL
func (gm *Manager) extractPortFromURL() int {
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
func (gm *Manager) GetGatewayURL() string {
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
