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
	sshClient *SSHClient
	cfg       *config.WebSSHConfig
	server    *config.SSHServerConfig
}

// NewRemoteInstaller creates a new remote installer
func NewRemoteInstaller(client *SSHClient, cfg *config.WebSSHConfig, server *config.SSHServerConfig) *RemoteInstaller {
	return &RemoteInstaller{
		sshClient: client,
		cfg:       cfg,
		server:    server,
	}
}

// EnsureBinary checks if infer exists on remote server, installs if missing
func (i *RemoteInstaller) EnsureBinary() error {
	// Check if auto-install is enabled
	autoInstall := i.cfg.AutoInstall
	if i.server.AutoInstall != nil {
		autoInstall = *i.server.AutoInstall
	}

	if !autoInstall {
		logger.Info("Auto-install disabled, skipping binary check", "server", i.server.Name)
		return nil
	}

	logger.Info("Checking if infer binary exists on remote server", "server", i.server.Name)

	// Check if binary exists
	exists, err := i.checkBinaryExists()
	if err != nil {
		return fmt.Errorf("failed to check if binary exists: %w", err)
	}

	if exists {
		logger.Info("Infer binary already exists on remote server", "server", i.server.Name)
		return nil
	}

	logger.Info("Infer binary not found, installing...", "server", i.server.Name)

	// Install binary
	if err := i.installBinary(); err != nil {
		return fmt.Errorf("failed to install binary: %w", err)
	}

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

	// Try to run: command -v <binary>
	cmd := fmt.Sprintf("command -v %s", commandPath)
	output, err := session.CombinedOutput(cmd)

	if err != nil {
		// Command failed, binary doesn't exist
		logger.Info("Binary not found", "command", commandPath, "output", string(output))
		return false, nil
	}

	// Binary exists
	logger.Info("Binary found", "path", strings.TrimSpace(string(output)))
	return true, nil
}

// installBinary downloads and installs infer binary on remote server using the official install script
func (i *RemoteInstaller) installBinary() error {
	// Get version to install
	version := i.cfg.InstallVersion
	var err error
	if version == "latest" || version == "" {
		version, err = i.getLatestVersion()
		if err != nil {
			return fmt.Errorf("failed to get latest version: %w", err)
		}
	}

	logger.Info("Installing version using install script", "version", version, "server", i.server.Name)

	// Determine install directory
	installDir := "$HOME/bin"
	if i.server.InstallPath != "" {
		installDir = strings.TrimSuffix(i.server.InstallPath, "/infer")
	}

	// Use the official install script which handles OS/arch detection and downloads the right binary
	installScript := fmt.Sprintf(`
set -e
mkdir -p %s
echo "Downloading and running install script for version v%s..."
curl -fsSL https://raw.githubusercontent.com/inference-gateway/cli/main/install.sh | bash -s -- --version v%s --install-dir %s
echo "Installation complete!"
echo "Binary installed to: %s/infer"
if ! echo $PATH | grep -q "%s"; then
    echo "Note: Add %s to your PATH to use 'infer' command globally"
fi
`, installDir, version, version, installDir, installDir, installDir, installDir)

	logger.Info("Running installation script", "server", i.server.Name)

	// Execute installation script
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

	// Verify installation
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

	// Set User-Agent to avoid GitHub API rate limiting
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

	// Remove 'v' prefix if present
	version := strings.TrimPrefix(release.TagName, "v")

	logger.Info("Latest version detected", "version", version)

	return version, nil
}
