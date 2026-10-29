package daemon

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// LocalTokenFile keeps the binding token this machine's daemon and its local
// clients share when daemon.yaml configures none.
func LocalTokenFile() string {
	return filepath.Join(config.UserSpaceConfigDir(), "daemon", "binding.token")
}

// FillBindingToken gives an enabled binding with neither a token nor client
// certificates this machine's local token, created on first use, so the
// daemon and its local clients agree on one without daemon.yaml.
func FillBindingToken(cfg *config.Config) error {
	if binding := cfg.Binding(); !binding.Enabled || binding.Dialable() {
		return nil
	}
	token, err := localToken(LocalTokenFile())
	if err != nil {
		return fmt.Errorf("reading the local binding token %s: %w", LocalTokenFile(), err)
	}
	cfg.Daemon.Binding.Token = token
	logger.Debug("the binding uses this machine's local token", "file", LocalTokenFile())
	return nil
}

// localToken reads the token at path, creating it owner-only when missing.
// The token is linked into place whole, so racing creators settle on the
// first one and nobody reads a half-written file.
func localToken(path string) (string, error) {
	if token, err := readToken(path); !errors.Is(err, fs.ErrNotExist) {
		return token, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".binding-token-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	_, err = tmp.WriteString(rand.Text())
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	if err := os.Link(tmp.Name(), path); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", err
	}
	return readToken(path)
}

func readToken(path string) (string, error) {
	data, err := os.ReadFile(path)
	token := strings.TrimSpace(string(data))
	if err == nil && token == "" {
		return "", errors.New("the file is empty, delete it to create a new token")
	}
	return token, err
}
