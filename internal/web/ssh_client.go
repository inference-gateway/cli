package web

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	ssh "golang.org/x/crypto/ssh"
	agent "golang.org/x/crypto/ssh/agent"
	knownhosts "golang.org/x/crypto/ssh/knownhosts"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// SSHClient manages SSH connections to remote servers
type SSHClient struct {
	cfg    *config.WebSSHConfig
	server *config.SSHServerConfig
	client *ssh.Client
}

// NewSSHClient creates an SSH client for the specified server
func NewSSHClient(cfg *config.WebSSHConfig, server *config.SSHServerConfig) (*SSHClient, error) {
	if server == nil {
		return nil, fmt.Errorf("server configuration is required")
	}

	return &SSHClient{
		cfg:    cfg,
		server: server,
	}, nil
}

// Connect establishes SSH connection to the remote server
func (c *SSHClient) Connect() error {
	sshConfig, err := c.getSSHConfig()
	if err != nil {
		return fmt.Errorf("failed to create SSH config: %w", err)
	}

	addr := net.JoinHostPort(c.server.RemoteHost, fmt.Sprintf("%d", c.server.RemotePort))

	logger.Info("Connecting to SSH server",
		"host", c.server.RemoteHost,
		"port", c.server.RemotePort,
		"user", c.server.RemoteUser)

	conn, err := net.DialTimeout("tcp", addr, 30*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", addr, err)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, sshConfig)
	if err != nil {
		if closeErr := conn.Close(); closeErr != nil {
			logger.Warn("Failed to close connection after SSH handshake failure", "error", closeErr)
		}
		return fmt.Errorf("failed to establish SSH connection: %w", err)
	}

	c.client = ssh.NewClient(sshConn, chans, reqs)

	logger.Info("SSH connection established", "server", c.server.Name)
	return nil
}

// NewSession creates a new SSH session
func (c *SSHClient) NewSession() (*ssh.Session, error) {
	if c.client == nil {
		return nil, fmt.Errorf("SSH client not connected")
	}

	session, err := c.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create SSH session: %w", err)
	}

	return session, nil
}

// Close closes the SSH connection
func (c *SSHClient) Close() error {
	if c.client != nil {
		logger.Info("Closing SSH connection", "server", c.server.Name)
		return c.client.Close()
	}
	return nil
}

// getSSHConfig creates SSH client configuration with authentication
func (c *SSHClient) getSSHConfig() (*ssh.ClientConfig, error) {
	var signers []ssh.Signer
	var err error

	signers, err = connectSSHAgent()
	if err != nil {
		logger.Warn("SSH agent not available, falling back to key files", "error", err)

		signers, err = loadSSHKeysFromFiles()
		if err != nil {
			return nil, fmt.Errorf("failed to load SSH keys: %w (tried SSH agent and key files)", err)
		}
		logger.Info("Loaded SSH keys from files", "keys_available", len(signers))
	} else {
		logger.Info("SSH agent connected", "keys_available", len(signers))
	}

	if len(signers) == 0 {
		return nil, fmt.Errorf("no SSH keys found (tried SSH agent and ~/.ssh key files)")
	}

	var hostKeyCallback ssh.HostKeyCallback
	if c.cfg.KnownHostsPath != "" {
		knownHostsPath := expandPath(c.cfg.KnownHostsPath)
		hostKeyCallback, err = knownhosts.New(knownHostsPath)
		if err != nil {
			logger.Warn("Failed to load known_hosts, using insecure connection",
				"path", knownHostsPath,
				"error", err)
			hostKeyCallback = ssh.InsecureIgnoreHostKey()
		} else {
			logger.Info("Using known_hosts for host key verification", "path", knownHostsPath)
		}
	} else {
		logger.Warn("No known_hosts path configured, using insecure connection")
		hostKeyCallback = ssh.InsecureIgnoreHostKey()
	}

	return &ssh.ClientConfig{
		User: c.server.RemoteUser,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signers...),
		},
		HostKeyCallback: hostKeyCallback,
		Timeout:         30 * time.Second,
	}, nil
}

// connectSSHAgent connects to the SSH agent and returns available signers
func connectSSHAgent() ([]ssh.Signer, error) {
	agentSock := os.Getenv("SSH_AUTH_SOCK")
	if agentSock == "" {
		return nil, fmt.Errorf("SSH_AUTH_SOCK environment variable not set")
	}

	conn, err := net.Dial("unix", agentSock)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SSH agent at %s: %w", agentSock, err)
	}

	agentClient := agent.NewClient(conn)
	signers, err := agentClient.Signers()
	if err != nil {
		if closeErr := conn.Close(); closeErr != nil {
			logger.Warn("Failed to close agent connection after error", "error", closeErr)
		}
		return nil, fmt.Errorf("failed to get signers from SSH agent: %w", err)
	}

	return signers, nil
}

// loadSSHKeysFromFiles loads SSH keys from standard file locations
func loadSSHKeysFromFiles() ([]ssh.Signer, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	keyFiles := []string{
		filepath.Join(homeDir, ".ssh", "id_rsa"),
		filepath.Join(homeDir, ".ssh", "id_ed25519"),
		filepath.Join(homeDir, ".ssh", "id_ecdsa"),
	}

	var signers []ssh.Signer
	for _, keyFile := range keyFiles {
		signer, err := loadPrivateKeyFile(keyFile)
		if err != nil {
			logger.Debug("Failed to load key file", "file", keyFile, "error", err)
			continue
		}
		logger.Info("Loaded SSH key from file", "file", keyFile)
		signers = append(signers, signer)
	}

	if len(signers) == 0 {
		return nil, fmt.Errorf("no SSH keys found in %s/.ssh/ (tried id_rsa, id_ed25519, id_ecdsa)", homeDir)
	}

	return signers, nil
}

// loadPrivateKeyFile loads a private key from a file
func loadPrivateKeyFile(path string) (ssh.Signer, error) {
	keyData, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	signer, err := ssh.ParsePrivateKey(keyData)
	if err == nil {
		return signer, nil
	}

	return nil, fmt.Errorf("failed to parse key (may be encrypted): %w", err)
}

// expandPath expands ~ to home directory
func expandPath(path string) string {
	if len(path) == 0 || path[0] != '~' {
		return path
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return path
	}

	if len(path) == 1 {
		return homeDir
	}

	return filepath.Join(homeDir, path[1:])
}
