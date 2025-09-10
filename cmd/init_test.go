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
		flags       map[string]bool
		wantFiles   []string
		wantNoFiles []string
		wantErr     bool
	}{
		{
			name: "basic project initialization",
			flags: map[string]bool{
				"overwrite":      false,
				"userspace":      false,
				"skip-agents-md": false,
			},
			wantFiles:   []string{".infer/config.yaml", ".infer/.gitignore", "AGENTS.md"},
			wantNoFiles: []string{},
			wantErr:     false,
		},
		{
			name: "project initialization with skip-agents-md",
			flags: map[string]bool{
				"overwrite":      false,
				"userspace":      false,
				"skip-agents-md": true,
			},
			wantFiles:   []string{".infer/config.yaml", ".infer/.gitignore"},
			wantNoFiles: []string{"AGENTS.md"},
			wantErr:     false,
		},
		{
			name: "userspace initialization",
			flags: map[string]bool{
				"overwrite":      true,
				"userspace":      true,
				"skip-agents-md": true,
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
				cmd.Flags().Bool(flag, value, "")
				_ = cmd.Flag(flag).Value.Set(strconv.FormatBool(value))
			}

			err = initializeProject(cmd)

			if (err != nil) != tt.wantErr {
				t.Errorf("initializeProject() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			for _, file := range tt.wantFiles {
				if tt.flags["userspace"] && !strings.Contains(file, "/") {
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
		expectExists bool
		wantErr      bool
	}{
		{
			name:         "project AGENTS.md generation",
			userspace:    false,
			expectExists: true,
			wantErr:      false,
		},
		{
			name:         "userspace AGENTS.md generation",
			userspace:    true,
			expectExists: true,
			wantErr:      false,
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

			err = generateAgentsMD(agentsMDPath, tt.userspace, "")

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

func TestGetDefaultAgentsMDContent(t *testing.T) {
	content := getDefaultAgentsMDContent()

	expectedSections := []string{
		"# AGENTS.md",
		"## Project Overview",
		"## Development Environment",
		"## Development Workflow",
		"## Key Commands",
		"## Testing Instructions",
		"## Project Conventions",
		"## Important Files & Configurations",
	}

	for _, section := range expectedSections {
		if !strings.Contains(content, section) {
			t.Errorf("default AGENTS.md content missing section: %s", section)
		}
	}

	if len(content) < 500 {
		t.Errorf("default AGENTS.md content seems too short: %d characters", len(content))
	}
}

func TestGetProjectAnalysisModel(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected string
	}{
		{
			name:     "default model when no env var",
			envValue: "",
			expected: "anthropic/claude-3-haiku",
		},
		{
			name:     "custom model from env var",
			envValue: "openai/gpt-4",
			expected: "openai/gpt-4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				_ = os.Setenv("INFER_AGENT_MODEL", tt.envValue)
				defer func() { _ = os.Unsetenv("INFER_AGENT_MODEL") }()
			} else {
				_ = os.Unsetenv("INFER_AGENT_MODEL")
			}

			result := getProjectAnalysisModel()
			if result != tt.expected {
				t.Errorf("getProjectAnalysisModel() = %v, want %v", result, tt.expected)
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
