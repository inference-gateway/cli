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
// and no warnings; a malformed file yields no keys plus a warning.
// Warnings (malformed JSON, permissions broader than 0600) are returned
// for the caller to log, so the fallback degrades silently and never
// blocks startup. config cannot import the platform logger (the logger
// imports config), hence the returned warnings instead.
func LoadAuthKeys() (map[string]string, []string) {
	path := AuthFilePath()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}

	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]string{}, nil
	}

	keys := map[string]string{}
	if err := json.Unmarshal(data, &keys); err != nil {
		// Drop everything, even partially-parsed entries: half-trusting a
		// malformed credential file is worse than ignoring it.
		return nil, []string{fmt.Sprintf("ignoring malformed %s: %v", path, err)}
	}

	if info, err := os.Stat(path); err == nil && info.Mode().Perm()&0077 != 0 {
		return keys, []string{fmt.Sprintf("%s permissions are broader than 0600 (%s)", path, info.Mode().Perm())}
	}

	return keys, nil
}
