package services

import (
	"os"
	"path/filepath"
	"testing"

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
	am := NewAgentManager(sessionID, cfg, agentsConfig)

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
	am := NewAgentManager(sessionID, cfg, agentsConfig)

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
	am := NewAgentManager(sessionID, cfg, agentsConfig)

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
	am := NewAgentManager(sessionID, cfg, agentsConfig)

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
	am := NewAgentManager(sessionID, cfg, agentsConfig)

	envMap, err := am.loadDotEnvFile()
	require.NoError(t, err)
	require.NotNil(t, envMap)

	require.Equal(t, "sk-test-123", envMap["DEEPSEEK_API_KEY"])
	require.Equal(t, "sk-ant-test-456", envMap["ANTHROPIC_API_KEY"])
	require.Equal(t, "debug", envMap["LOG_LEVEL"])
	require.Len(t, envMap, 3)
}
