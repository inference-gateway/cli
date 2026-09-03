package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// AuthFileName is the userspace provider-key file: a flat JSON map of
// provider API key env vars to values, e.g. {"OPENAI_API_KEY": "sk-..."}.
const AuthFileName = "auth.json"

// AuthFilePath returns the userspace auth file path (~/.infer/auth.json).
func AuthFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ConfigDirName, AuthFileName)
}

// LoadAuthKeys reads the userspace auth file as the lowest-precedence
// fallback source of provider API keys (after the system environment and
// the project .env). A missing, unreadable, or empty file yields no keys
// and no error; a malformed file yields no keys plus an error. The error
// is a warning for the caller to log (config cannot import the platform
// logger, which imports config), so the fallback never blocks startup.
func LoadAuthKeys() (map[string]string, error) {
	path := AuthFilePath()

	data, err := os.ReadFile(path)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}

	keys := map[string]string{}
	if err := json.Unmarshal(data, &keys); err != nil {
		return nil, fmt.Errorf("ignoring malformed %s: %w", path, err)
	}

	if info, err := os.Stat(path); err == nil && info.Mode().Perm()&0077 != 0 {
		return keys, fmt.Errorf("%s permissions are broader than 0600 (%s)", path, info.Mode().Perm())
	}

	return keys, nil
}
