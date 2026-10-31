package config

import (
	"bytes"
	"cmp"
	"fmt"
	"os"
	"path/filepath"

	yaml "gopkg.in/yaml.v3"
)

// AuthFileName is the userspace provider-key file: a flat map of provider API
// key env vars to values, e.g. `OPENAI_API_KEY: sk-...`. YAML, like every
// other hand-edited file under ~/.infer.
const AuthFileName = "auth.yaml"

// AuthFilePath returns the userspace auth file path (~/.infer/auth.yaml).
func AuthFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ConfigDirName, AuthFileName)
}

// LoadAuthKeys reads the userspace auth file as the lowest-precedence
// fallback source of provider API keys (after the system environment and
// the project .env). A missing, unreadable, or empty file yields no keys and
// no error; a malformed file yields no keys plus an error. The error is a
// warning for the caller to log (config cannot import the platform logger,
// which imports config), so the fallback never blocks startup.
func LoadAuthKeys() (map[string]string, error) {
	path := AuthFilePath()

	data, err := os.ReadFile(path)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}

	keys := map[string]string{}
	if err := yaml.Unmarshal(data, &keys); err != nil {
		return nil, fmt.Errorf("ignoring malformed %s: %w", path, err)
	}

	if info, err := os.Stat(path); err == nil && info.Mode().Perm()&0077 != 0 {
		return keys, fmt.Errorf("%s permissions are broader than 0600 (%s)", path, info.Mode().Perm())
	}

	return keys, nil
}

// Credentials resolves names the way the local gateway does: the environment
// first, then the userspace auth file. Unset names are left out, and the error
// is the auth file's warning, so callers log it and go on.
func Credentials(names []string) (map[string]string, error) {
	authKeys, err := LoadAuthKeys()
	values := make(map[string]string, len(names))
	for _, name := range names {
		if value := cmp.Or(os.Getenv(name), authKeys[name]); value != "" {
			values[name] = value
		}
	}
	return values, err
}
