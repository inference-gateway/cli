package cmd

import (
	"os"
	"path/filepath"
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
				"overwrite": false,
				"userspace": false,
				"model":     "",
			},
			wantFiles:   []string{".infer/config.yaml", ".infer/.gitignore"},
			wantNoFiles: []string{"AGENTS.md"},
			wantErr:     false,
		},
		{
			name: "project initialization with model",
			flags: map[string]any{
				"overwrite": false,
				"userspace": false,
				"model":     "anthropic/claude-3-haiku",
			},
			wantFiles:   []string{".infer/config.yaml", ".infer/.gitignore"},
			wantNoFiles: []string{},
			wantErr:     true,
		},
		{
			name: "userspace initialization",
			flags: map[string]any{
				"overwrite": true,
				"userspace": true,
				"model":     "",
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

func TestGenerateAgentsMD(t *testing.T) {
	tests := []struct {
		name         string
		userspace    bool
		model        string
		expectExists bool
		wantErr      bool
	}{
		{
			name:         "project AGENTS.md generation without model",
			userspace:    false,
			model:        "",
			expectExists: false,
			wantErr:      true,
		},
		{
			name:         "project AGENTS.md generation with model",
			userspace:    false,
			model:        "anthropic/claude-3-haiku",
			expectExists: false,
			wantErr:      true,
		},
		{
			name:         "userspace AGENTS.md generation without model",
			userspace:    true,
			model:        "",
			expectExists: false,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "infer-agents-md-test-*")
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

			agentsMDPath := filepath.Join(tmpDir, "AGENTS.md")

			err = generateAgentsMD(agentsMDPath, tt.userspace, tt.model)

			if (err != nil) != tt.wantErr {
				t.Errorf("generateAgentsMD() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.expectExists {
				return
			}

			if _, err := os.Stat(agentsMDPath); os.IsNotExist(err) {
				t.Errorf("expected AGENTS.md to be created, but it wasn't")
				return
			}

			content, err := os.ReadFile(agentsMDPath)
			if err != nil {
				t.Errorf("failed to read AGENTS.md: %v", err)
				return
			}

			contentStr := string(content)
			if !strings.Contains(contentStr, "# AGENTS.md") {
				t.Errorf("AGENTS.md does not contain expected header")
			}

			if !strings.Contains(contentStr, "## Project Overview") {
				t.Errorf("AGENTS.md does not contain expected Project Overview section")
			}
		})
	}
}


func TestProjectResearchSystemPrompt(t *testing.T) {
	prompt := projectResearchSystemPrompt()

	expectedKeywords := []string{
		"project analysis agent",
		"ANALYSIS OBJECTIVES",
		"OUTPUT FORMAT",
		"RESEARCH APPROACH",
		"IMPORTANT GUIDELINES",
		"AGENTS.md",
	}

	for _, keyword := range expectedKeywords {
		if !strings.Contains(prompt, keyword) {
			t.Errorf("project research system prompt missing keyword: %s", keyword)
		}
	}

	if len(prompt) < 1000 {
		t.Errorf("project research system prompt seems too short: %d characters", len(prompt))
	}
}
