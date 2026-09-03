package config

import (
	"os"
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"
)

func writeAuthFile(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)

	authDir := filepath.Join(home, ConfigDirName)
	if err := os.MkdirAll(authDir, 0755); err != nil {
		t.Fatal(err)
	}

	authPath := filepath.Join(authDir, AuthFileName)
	if err := os.WriteFile(authPath, []byte(content), mode); err != nil {
		t.Fatal(err)
	}

	return authPath
}

func TestAuthFilePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	require.Equal(t, filepath.Join(home, ConfigDirName, AuthFileName), AuthFilePath())
}

func TestLoadAuthKeys(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T)
		wantKeys map[string]string
		wantErr  string
	}{
		{
			name:     "missing file degrades silently",
			setup:    func(t *testing.T) { t.Setenv("HOME", t.TempDir()) },
			wantKeys: nil,
		},
		{
			name: "empty file degrades silently",
			setup: func(t *testing.T) {
				writeAuthFile(t, "", 0600)
			},
			wantKeys: nil,
		},
		{
			name: "malformed file yields warning and no keys",
			setup: func(t *testing.T) {
				writeAuthFile(t, `{"OPENAI_API_KEY": 123}`, 0600)
			},
			wantKeys: nil,
			wantErr:  "malformed",
		},
		{
			name: "valid file yields keys",
			setup: func(t *testing.T) {
				writeAuthFile(t, `{"ANTHROPIC_API_KEY": "sk-ant-...", "OPENAI_API_KEY": "sk-..."}`, 0600)
			},
			wantKeys: map[string]string{
				"ANTHROPIC_API_KEY": "sk-ant-...",
				"OPENAI_API_KEY":    "sk-...",
			},
		},
		{
			name: "broad permissions yield warning",
			setup: func(t *testing.T) {
				writeAuthFile(t, `{"OPENAI_API_KEY": "sk-..."}`, 0644)
			},
			wantKeys: map[string]string{"OPENAI_API_KEY": "sk-..."},
			wantErr:  "broader than 0600",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup(t)

			keys, err := LoadAuthKeys()

			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantErr)
			}
			require.Equal(t, tt.wantKeys, keys)
		})
	}
}

func TestLoadAuthKeys_UnreadableFile(t *testing.T) {
	authPath := writeAuthFile(t, `{"OPENAI_API_KEY": "sk-..."}`, 0000)

	keys, err := LoadAuthKeys()

	require.Nil(t, keys)
	require.NoError(t, err)
	require.FileExists(t, authPath)
}
