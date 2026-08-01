package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	require "github.com/stretchr/testify/require"
)

func TestAgentManager_loadDotEnvFile(t *testing.T) {
	tmpDir := t.TempDir()

	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		_ = os.Chdir(originalWd)
	}()

	require.NoError(t, os.Chdir(tmpDir))

	dotEnvContent := `DEEPSEEK_API_KEY=sk-test-123
ANTHROPIC_API_KEY=sk-ant-test-456
LOG_LEVEL=debug
PORT=8080
`
	dotEnvPath := filepath.Join(tmpDir, ".env")
	require.NoError(t, os.WriteFile(dotEnvPath, []byte(dotEnvContent), 0644))

	cfg := &config.Config{}
	agentsConfig := &config.AgentsConfig{}
	sessionID := domain.GenerateSessionID()
	am := NewAgentManager(sessionID, cfg, agentsConfig, nil, nil)

	envMap, err := am.loadDotEnvFile()
	require.NoError(t, err)
	require.NotNil(t, envMap)

	require.Equal(t, "sk-test-123", envMap["DEEPSEEK_API_KEY"])
	require.Equal(t, "sk-ant-test-456", envMap["ANTHROPIC_API_KEY"])
	require.Equal(t, "debug", envMap["LOG_LEVEL"])
	require.Equal(t, "8080", envMap["PORT"])
	require.Len(t, envMap, 4)
}

func TestAgentManager_loadDotEnvFile_NotFound(t *testing.T) {
	tmpDir := t.TempDir()

	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		_ = os.Chdir(originalWd)
	}()

	require.NoError(t, os.Chdir(tmpDir))

	cfg := &config.Config{}
	agentsConfig := &config.AgentsConfig{}
	sessionID := domain.GenerateSessionID()
	am := NewAgentManager(sessionID, cfg, agentsConfig, nil, nil)

	envMap, err := am.loadDotEnvFile()
	require.Error(t, err)
	require.Nil(t, envMap)
	require.Contains(t, err.Error(), ".env file not found")
}

func TestAgentManager_loadDotEnvFile_InvalidFormat(t *testing.T) {
	tmpDir := t.TempDir()

	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		_ = os.Chdir(originalWd)
	}()

	require.NoError(t, os.Chdir(tmpDir))

	dotEnvPath := filepath.Join(tmpDir, ".env")
	require.NoError(t, os.WriteFile(dotEnvPath, []byte{0xFF, 0xFE, 0xFD}, 0644))

	cfg := &config.Config{}
	agentsConfig := &config.AgentsConfig{}
	sessionID := domain.GenerateSessionID()
	am := NewAgentManager(sessionID, cfg, agentsConfig, nil, nil)

	envMap, err := am.loadDotEnvFile()
	if err == nil {
		require.NotNil(t, envMap)
	}
}

func TestAgentManager_loadDotEnvFile_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()

	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		_ = os.Chdir(originalWd)
	}()

	require.NoError(t, os.Chdir(tmpDir))

	dotEnvPath := filepath.Join(tmpDir, ".env")
	require.NoError(t, os.WriteFile(dotEnvPath, []byte(""), 0644))

	cfg := &config.Config{}
	agentsConfig := &config.AgentsConfig{}
	sessionID := domain.GenerateSessionID()
	am := NewAgentManager(sessionID, cfg, agentsConfig, nil, nil)

	envMap, err := am.loadDotEnvFile()
	require.NoError(t, err)
	require.NotNil(t, envMap)
	require.Len(t, envMap, 0)
}

func TestAgentManager_loadDotEnvFile_WithComments(t *testing.T) {
	tmpDir := t.TempDir()

	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		_ = os.Chdir(originalWd)
	}()

	require.NoError(t, os.Chdir(tmpDir))

	dotEnvContent := `# API Keys
DEEPSEEK_API_KEY=sk-test-123
# Anthropic key
ANTHROPIC_API_KEY=sk-ant-test-456

# Configuration
LOG_LEVEL=debug
`
	dotEnvPath := filepath.Join(tmpDir, ".env")
	require.NoError(t, os.WriteFile(dotEnvPath, []byte(dotEnvContent), 0644))

	cfg := &config.Config{}
	agentsConfig := &config.AgentsConfig{}
	sessionID := domain.GenerateSessionID()
	am := NewAgentManager(sessionID, cfg, agentsConfig, nil, nil)

	envMap, err := am.loadDotEnvFile()
	require.NoError(t, err)
	require.NotNil(t, envMap)

	require.Equal(t, "sk-test-123", envMap["DEEPSEEK_API_KEY"])
	require.Equal(t, "sk-ant-test-456", envMap["ANTHROPIC_API_KEY"])
	require.Equal(t, "debug", envMap["LOG_LEVEL"])
	require.Len(t, envMap, 3)
}

func TestSetURLPort(t *testing.T) {
	cases := []struct {
		in   string
		port int
		want string
	}{
		{"http://localhost:8084", 9090, "http://localhost:9090"},
		{"http://localhost", 8081, "http://localhost:8081"},
		{"https://artifacts.internal:8084/base", 9090, "https://artifacts.internal:9090/base"},
		{"::not a url::", 9090, "::not a url::"},
	}
	for _, c := range cases {
		if got := setURLPort(c.in, c.port); got != c.want {
			t.Errorf("setURLPort(%q, %d) = %q, want %q", c.in, c.port, got, c.want)
		}
	}
}

func TestAgentManager_WaitForAgentsReady(t *testing.T) {
	am := NewAgentManager("session", &config.Config{}, &config.AgentsConfig{}, nil, nil)

	done := make(chan struct{})
	go func() {
		am.WaitForAgentsReady(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("WaitForAgentsReady should return immediately with no agents starting")
	}

	am.startWg.Add(1)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	am.WaitForAgentsReady(ctx)
	require.Less(t, time.Since(start), time.Second, "WaitForAgentsReady should respect ctx cancellation")
	am.startWg.Done()
}
