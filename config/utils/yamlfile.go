package utils

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	yaml "gopkg.in/yaml.v3"
)

// LoadYAML reads path to run with: ${VAR} references resolve from the
// environment before unmarshalling, and a literal `${…}` must be escaped as
// `$$…`. A missing file yields defaults(). label scopes error messages, e.g.
// "channels" produces "failed to read channels config: …".
func LoadYAML[T any](path, label string, defaults func() *T) (*T, error) {
	return loadYAML(path, label, defaults, os.ExpandEnv)
}

// LoadYAMLAsWritten reads path to edit: every ${VAR} reference stays as
// written, so saving the result back never puts a variable's value in its
// place. A missing file yields defaults().
func LoadYAMLAsWritten[T any](path, label string, defaults func() *T) (*T, error) {
	return loadYAML(path, label, defaults, func(text string) string { return text })
}

func loadYAML[T any](path, label string, defaults func() *T, expand func(string) string) (*T, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return defaults(), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s config: %w", label, err)
	}

	cfg := new(T)
	if err := yaml.Unmarshal([]byte(expand(string(data))), cfg); err != nil {
		return nil, fmt.Errorf("failed to parse %s config: %w", label, err)
	}

	return cfg, nil
}

// SaveYAML writes cfg to path, creating any missing parent directories.
// It always emits the YAML document marker `---\n` and uses 2-space indent.
func SaveYAML[T any](path, label string, cfg *T) error {
	var buf bytes.Buffer
	buf.WriteString("---\n")

	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)

	if err := encoder.Encode(cfg); err != nil {
		return fmt.Errorf("failed to marshal %s config: %w", label, err)
	}

	if err := encoder.Close(); err != nil {
		return fmt.Errorf("failed to close %s encoder: %w", label, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create %s config directory: %w", label, err)
	}

	if err := writeFileAtomic(path, buf.Bytes()); err != nil {
		return fmt.Errorf("failed to write %s config: %w", label, err)
	}

	return nil
}

// writeFileAtomic replaces path through a temp file and a rename, so a process
// reading it concurrently sees the old file or the new one, never a torn one.
// A symlinked path is written through to its target. The file keeps the mode
// it had, so one its owner tightened stays tight, and a new one gets 0644.
func writeFileAtomic(path string, data []byte) error {
	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target
	}
	mode := fs.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
