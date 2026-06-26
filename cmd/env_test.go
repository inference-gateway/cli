package cmd

import (
	"os"
	"strings"
	"testing"

	cobra "github.com/spf13/cobra"
)

func TestEnvExampleContent(t *testing.T) {
	content := envExampleContent()

	// Check that it contains the header
	if !strings.Contains(content, "# Inference Gateway Environment Variables") {
		t.Errorf("envExampleContent() should contain header")
	}

	// Check that it contains the cp hint
	if !strings.Contains(content, "cp .env.example .env") {
		t.Errorf("envExampleContent() should contain cp hint")
	}

	// Check that it contains all expected provider API keys
	expectedVars := []string{
		"ANTHROPIC_API_KEY=",
		"OPENAI_API_KEY=",
		"DEEPSEEK_API_KEY=",
		"GOOGLE_API_KEY=",
		"GROQ_API_KEY=",
		"MISTRAL_API_KEY=",
		"CLOUDFLARE_API_KEY=",
		"COHERE_API_KEY=",
		"OLLAMA_API_KEY=",
		"OLLAMA_CLOUD_API_KEY=",
		"GOOGLE_SEARCH_API_KEY=",
		"GOOGLE_SEARCH_ENGINE_ID=",
		"DUCKDUCKGO_SEARCH_API_KEY=",
		"MINIMAX_API_KEY=",
		"MOONSHOT_API_KEY=",
	}

	for _, v := range expectedVars {
		if !strings.Contains(content, v) {
			t.Errorf("envExampleContent() should contain %s", v)
		}
	}
}

func TestCreateEnvExample(t *testing.T) {
	tests := []struct {
		name           string
		setupFiles     map[string]string // files to create before test
		overwrite      bool
		wantErr        bool
		errContains    string
		wantFileExists bool
	}{
		{
			name:           "creates .env.example when it doesn't exist",
			setupFiles:     nil,
			overwrite:      false,
			wantErr:        false,
			wantFileExists: true,
		},
		{
			name: "fails when .env.example already exists",
			setupFiles: map[string]string{
				".env.example": "existing content",
			},
			overwrite:      false,
			wantErr:        true,
			errContains:    "already exists",
			wantFileExists: true,
		},
		{
			name: "overwrites when --overwrite is set",
			setupFiles: map[string]string{
				".env.example": "existing content",
			},
			overwrite:      true,
			wantErr:        false,
			wantFileExists: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "infer-env-test-*")
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

			// Setup pre-existing files
			for path, content := range tt.setupFiles {
				if err := os.WriteFile(path, []byte(content), 0644); err != nil {
					t.Fatalf("failed to create setup file %s: %v", path, err)
				}
			}

			cmd := &cobra.Command{}
			cmd.Flags().Bool("overwrite", tt.overwrite, "")
			_ = cmd.Flag("overwrite").Value.Set(func() string {
				if tt.overwrite {
					return "true"
				}
				return "false"
			}())

			err = createEnvExample(cmd)

			if (err != nil) != tt.wantErr {
				t.Errorf("createEnvExample() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errContains != "" && err != nil {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("createEnvExample() error should contain %q, got %q", tt.errContains, err.Error())
				}
			}

			if tt.wantFileExists {
				if _, err := os.Stat(".env.example"); os.IsNotExist(err) {
					t.Errorf("expected .env.example to exist, but it doesn't")
				}
			}
		})
	}
}

func TestCreateEnvExampleCreatesGitignore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "infer-env-gitignore-test-*")
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
	cmd.Flags().Bool("overwrite", false, "")
	_ = cmd.Flag("overwrite").Value.Set("false")

	err = createEnvExample(cmd)
	if err != nil {
		t.Fatalf("createEnvExample() error = %v", err)
	}

	// Check that .gitignore was created with .env entry
	gitignoreContent, err := os.ReadFile(".gitignore")
	if err != nil {
		t.Fatalf("expected .gitignore to be created, but got error: %v", err)
	}

	if !strings.Contains(string(gitignoreContent), ".env") {
		t.Errorf(".gitignore should contain .env entry, got: %s", string(gitignoreContent))
	}
}

func TestCreateEnvExampleWithExistingGitignore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "infer-env-existing-gitignore-test-*")
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

	// Create a .gitignore without .env
	if err := os.WriteFile(".gitignore", []byte("*.log\n"), 0644); err != nil {
		t.Fatalf("failed to create .gitignore: %v", err)
	}

	cmd := &cobra.Command{}
	cmd.Flags().Bool("overwrite", false, "")
	_ = cmd.Flag("overwrite").Value.Set("false")

	err = createEnvExample(cmd)
	if err != nil {
		t.Fatalf("createEnvExample() error = %v", err)
	}

	// Check that .env was added to .gitignore
	gitignoreContent, err := os.ReadFile(".gitignore")
	if err != nil {
		t.Fatalf("expected .gitignore to exist, but got error: %v", err)
	}

	if !strings.Contains(string(gitignoreContent), ".env") {
		t.Errorf(".gitignore should contain .env entry, got: %s", string(gitignoreContent))
	}
}

func TestEnvExampleContentFormat(t *testing.T) {
	content := envExampleContent()

	// Each line should either be empty, a comment, or a KEY= format
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, "=") {
			t.Errorf("line %d should be in KEY= format, got: %q", i+1, line)
		}
	}
}
