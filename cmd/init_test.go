package cmd

import (
	"os"
	"strconv"
	"strings"
	"testing"

	cobra "github.com/spf13/cobra"
)

func TestInitializeProject(t *testing.T) {
	tests := []struct {
		name        string
		flags       map[string]any
		wantFiles   []string
		wantNoFiles []string
		wantErr     bool
	}{
		{
			name: "basic project initialization",
			flags: map[string]any{
				"overwrite":        false,
				"userspace":        false,
				"skip-migrations":  true,
			},
			wantFiles:   []string{".infer/config.yaml", ".infer/.gitignore"},
			wantNoFiles: []string{"AGENTS.md"},
			wantErr:     false,
		},
		{
			name: "userspace initialization",
			flags: map[string]any{
				"overwrite":        true,
				"userspace":        true,
				"skip-migrations":  true,
			},
			wantFiles:   []string{},
			wantNoFiles: []string{".infer/config.yaml", ".infer/.gitignore", "AGENTS.md"},
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "infer-init-test-*")
			if err != nil {
				t.Fatalf("failed to create temp dir: %v", err)
			}
			defer func() { _ = os.RemoveAll(tmpDir) }()

			oldWd, err := os.Getwd()
			if err != nil {
				t.Fatalf("failed to get working directory: %v", err)
			}
			defer func() { _ = os.Chdir(oldWd) }()

			if err := os.Chdir(tmpDir); err != nil {
				t.Fatalf("failed to change to temp dir: %v", err)
			}

			cmd := &cobra.Command{}
			for flag, value := range tt.flags {
				switch v := value.(type) {
				case bool:
					cmd.Flags().Bool(flag, v, "")
					_ = cmd.Flag(flag).Value.Set(strconv.FormatBool(v))
				case string:
					cmd.Flags().String(flag, v, "")
					_ = cmd.Flag(flag).Value.Set(v)
				}
			}

			err = initializeProject(cmd)

			if (err != nil) != tt.wantErr {
				t.Errorf("initializeProject() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			for _, file := range tt.wantFiles {
				if userspace, ok := tt.flags["userspace"].(bool); ok && userspace && !strings.Contains(file, "/") {
					continue
				}
				if _, err := os.Stat(file); os.IsNotExist(err) {
					t.Errorf("expected file %s to exist, but it doesn't", file)
				}
			}

			for _, file := range tt.wantNoFiles {
				if _, err := os.Stat(file); !os.IsNotExist(err) {
					t.Errorf("expected file %s to not exist, but it does", file)
				}
			}
		})
	}
}

func TestWriteConfigAsYAMLWithIndent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "infer-config-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	configPath := tmpDir + "/.infer/config.yaml"

	err = writeConfigAsYAMLWithIndent(configPath, 2)
	if err != nil {
		t.Errorf("writeConfigAsYAMLWithIndent() error = %v", err)
		return
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Errorf("expected config file to be created")
		return
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Errorf("failed to read config file: %v", err)
		return
	}

	if !strings.Contains(string(content), "gateway:") {
		t.Errorf("config file does not contain expected gateway section")
	}
}

func TestCheckFileExists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "infer-check-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Test non-existent file
	err = checkFileExists(tmpDir+"/nonexistent.txt", "test file")
	if err != nil {
		t.Errorf("checkFileExists() should not error for non-existent file: %v", err)
	}

	// Test existing file
	existingFile := tmpDir + "/existing.txt"
	if err := os.WriteFile(existingFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	err = checkFileExists(existingFile, "test file")
	if err == nil {
		t.Errorf("checkFileExists() should error for existing file")
	}
}
