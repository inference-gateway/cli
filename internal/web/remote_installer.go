package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// RemoteInstaller handles auto-installation of infer binary on remote servers
type RemoteInstaller struct {
	sshClient  *SSHClient
	cfg        *config.WebSSHConfig
	server     *config.SSHServerConfig
	gatewayURL string
	progressCh chan<- string
}

// NewRemoteInstaller creates a new remote installer
func NewRemoteInstaller(client *SSHClient, cfg *config.WebSSHConfig, server *config.SSHServerConfig, gatewayURL string, progressCh chan<- string) *RemoteInstaller {
	return &RemoteInstaller{
		sshClient:  client,
		cfg:        cfg,
		server:     server,
		gatewayURL: gatewayURL,
		progressCh: progressCh,
	}
}

// sendProgress sends a progress message to the channel if it's set
func (i *RemoteInstaller) sendProgress(message string) {
	if i.progressCh != nil {
		select {
		case i.progressCh <- message:
		default:
			// Channel full or closed, skip message
		}
	}
}

// EnsureBinary checks if infer exists on remote server, installs if missing
func (i *RemoteInstaller) EnsureBinary() error {
	autoInstall := i.cfg.AutoInstall
	if i.server.AutoInstall != nil {
		autoInstall = *i.server.AutoInstall
	}

	if !autoInstall {
		logger.Info("Auto-install disabled, skipping binary check", "server", i.server.Name)
		return nil
	}

	i.sendProgress("Checking for infer binary on remote server...")
	logger.Info("Checking if infer binary exists on remote server", "server", i.server.Name)

	exists, err := i.checkBinaryExists()
	if err != nil {
		return fmt.Errorf("failed to check if binary exists: %w", err)
	}

	if exists {
		i.sendProgress("Infer binary found on remote server")
		logger.Info("Infer binary already exists on remote server", "server", i.server.Name)
		return nil
	}

	i.sendProgress("Infer binary not found, starting installation...")
	logger.Info("Infer binary not found, installing...", "server", i.server.Name)

	if err := i.installBinary(); err != nil {
		return fmt.Errorf("failed to install binary: %w", err)
	}

	i.sendProgress("Infer binary successfully installed")
	logger.Info("Infer binary successfully installed", "server", i.server.Name)
	return nil
}

// checkBinaryExists checks if infer binary exists on remote server
func (i *RemoteInstaller) checkBinaryExists() (bool, error) {
	commandPath := i.server.CommandPath
	if commandPath == "" {
		commandPath = "infer"
	}

	session, err := i.sshClient.NewSession()
	if err != nil {
		return false, fmt.Errorf("failed to create SSH session: %w", err)
	}
	defer func() { _ = session.Close() }()

	cmd := fmt.Sprintf("command -v %s", commandPath)
	output, err := session.CombinedOutput(cmd)

	if err != nil {
		logger.Info("Binary not found", "command", commandPath, "output", string(output))
		return false, nil
	}

	logger.Info("Binary found", "path", strings.TrimSpace(string(output)))
	return true, nil
}

// installBinary downloads and installs infer binary on remote server using the official install script
func (i *RemoteInstaller) installBinary() error {
	version := i.cfg.InstallVersion
	var err error
	if version == "latest" || version == "" {
		i.sendProgress("Fetching latest version from GitHub...")
		version, err = i.getLatestVersion()
		if err != nil {
			return fmt.Errorf("failed to get latest version: %w", err)
		}
		i.sendProgress(fmt.Sprintf("Latest version: v%s", version))
	}

	logger.Info("Installing version using install script", "version", version, "server", i.server.Name)

	installDir := i.cfg.InstallDir
	if i.server.InstallPath != "" {
		installDir = strings.TrimSuffix(i.server.InstallPath, "/infer")
	}

	installScript := fmt.Sprintf(`
set -e
echo "Downloading and running install script for version v%s..."
curl -fsSL https://raw.githubusercontent.com/inference-gateway/cli/main/install.sh | bash -s -- --version v%s --install-dir %s
echo "Installation complete!"
echo "Binary installed to: %s/infer"
`, version, version, installDir, installDir)

	i.sendProgress(fmt.Sprintf("Downloading infer v%s (this may take a minute)...", version))
	logger.Info("Running installation script", "server", i.server.Name)

	session, err := i.sshClient.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create SSH session: %w", err)
	}
	defer func() { _ = session.Close() }()

	output, err := session.CombinedOutput(installScript)
	if err != nil {
		return fmt.Errorf("installation failed: %w\nOutput: %s", err, string(output))
	}

	logger.Info("Installation output", "output", string(output))

	i.sendProgress("Verifying installation...")
	session2, err := i.sshClient.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create SSH session for verification: %w", err)
	}
	defer func() { _ = session2.Close() }()

	verifyCmd := fmt.Sprintf("%s/infer version", installDir)
	verifyOutput, err := session2.CombinedOutput(verifyCmd)
	if err != nil {
		return fmt.Errorf("installation verification failed: %w\nOutput: %s", err, string(verifyOutput))
	}

	logger.Info("Installation verified", "version_output", string(verifyOutput))
	i.sendProgress("Binary verified successfully")

	i.sendProgress("Initializing configuration...")
	session3, err := i.sshClient.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create SSH session for initialization: %w", err)
	}
	defer func() { _ = session3.Close() }()

	initCmd := fmt.Sprintf("%s/infer init --userspace", installDir)
	initOutput, err := session3.CombinedOutput(initCmd)
	if err != nil {
		logger.Warn("Failed to initialize infer config, may need manual setup", "error", err, "output", string(initOutput))
	} else {
		logger.Info("Infer configuration initialized", "output", string(initOutput))
	}

	i.sendProgress("Configuring environment...")
	session4, err := i.sshClient.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create SSH session for env setup: %w", err)
	}
	defer func() { _ = session4.Close() }()

	envSetupCmd := fmt.Sprintf(`
if ! grep -q "INFER_REMOTE_MANAGED" ~/.bashrc 2>/dev/null; then
  echo "" >> ~/.bashrc
  echo "# Infer CLI - Remote managed instance" >> ~/.bashrc
  echo "export INFER_REMOTE_MANAGED=true" >> ~/.bashrc
  echo "export INFER_GATEWAY_URL=%s" >> ~/.bashrc
fi`, i.gatewayURL)

	envOutput, err := session4.CombinedOutput(envSetupCmd)
	if err != nil {
		logger.Warn("Failed to setup environment variables in profile", "error", err, "output", string(envOutput))
	} else {
		logger.Info("Environment variables configured in user profile")
	}

	return nil
}

// getLatestVersion fetches the latest version from GitHub releases API
func (i *RemoteInstaller) getLatestVersion() (string, error) {
	apiURL := "https://api.github.com/repos/inference-gateway/cli/releases/latest"

	logger.Info("Fetching latest version from GitHub", "url", apiURL)

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "inference-gateway-cli")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch latest version: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	var release struct {
		TagName string `json:"tag_name"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("failed to parse release info: %w", err)
	}

	version := strings.TrimPrefix(release.TagName, "v")

	logger.Info("Latest version detected", "version", version)

	return version, nil
}
