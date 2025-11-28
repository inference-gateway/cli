package shortcuts

import (
	"context"
	"testing"

	"github.com/inference-gateway/cli/config"
)

func TestSCMShortcut_GetName(t *testing.T) {
	shortcut := NewSCMShortcut(nil, &config.Config{}, nil)

	expected := "scm"
	actual := shortcut.GetName()

	if actual != expected {
		t.Errorf("Expected name %s, got %s", expected, actual)
	}
}

func TestSCMShortcut_GetDescription(t *testing.T) {
	shortcut := NewSCMShortcut(nil, &config.Config{}, nil)

	expected := "Source control management (e.g., /scm pr create, /scm issues)"
	actual := shortcut.GetDescription()

	if actual != expected {
		t.Errorf("Expected description %s, got %s", expected, actual)
	}
}

func TestSCMShortcut_GetUsage(t *testing.T) {
	shortcut := NewSCMShortcut(nil, &config.Config{}, nil)

	actual := shortcut.GetUsage()

	// Check that usage contains all expected commands
	expectedPhrases := []string{
		"/scm pr create",
		"/scm issues",
		"/scm issues <number>",
	}

	for _, phrase := range expectedPhrases {
		if !contains(actual, phrase) {
			t.Errorf("Expected usage to contain %q, got %s", phrase, actual)
		}
	}
}

func TestSCMShortcut_CanExecute(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected bool
	}{
		{
			name:     "no arguments not allowed",
			args:     []string{},
			expected: false,
		},
		{
			name:     "single pr argument not allowed",
			args:     []string{"pr"},
			expected: false,
		},
		{
			name:     "pr create allowed",
			args:     []string{"pr", "create"},
			expected: true,
		},
		{
			name:     "pr other not allowed",
			args:     []string{"pr", "delete"},
			expected: false,
		},
		{
			name:     "issues allowed",
			args:     []string{"issues"},
			expected: true,
		},
		{
			name:     "issues with number allowed",
			args:     []string{"issues", "123"},
			expected: true,
		},
		{
			name:     "other subcommand not allowed",
			args:     []string{"branch", "create"},
			expected: false,
		},
	}

	shortcut := NewSCMShortcut(nil, &config.Config{}, nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shortcut.CanExecute(tt.args)
			if result != tt.expected {
				t.Errorf("Expected CanExecute(%v) = %v, got %v", tt.args, tt.expected, result)
			}
		})
	}
}

func TestSCMShortcut_Execute_InvalidArgs(t *testing.T) {
	shortcut := NewSCMShortcut(nil, &config.Config{}, nil)

	result, err := shortcut.Execute(context.Background(), []string{})
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if result.Success {
		t.Error("Expected failure for empty args")
	}
}

func TestSCMShortcut_Execute_UnknownCommand(t *testing.T) {
	shortcut := NewSCMShortcut(nil, &config.Config{}, nil)

	result, err := shortcut.Execute(context.Background(), []string{"unknown", "command"})
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if result.Success {
		t.Error("Expected failure for unknown command")
	}

	if result.Output == "" {
		t.Error("Expected error message in output")
	}
}

func TestSCMShortcut_TruncateDiff(t *testing.T) {
	tests := []struct {
		name     string
		diff     string
		maxLen   int
		expected string
	}{
		{
			name:     "short diff unchanged",
			diff:     "short diff",
			maxLen:   100,
			expected: "short diff",
		},
		{
			name:     "long diff truncated",
			diff:     "this is a very long diff that should be truncated",
			maxLen:   20,
			expected: "this is a very long \n... (diff truncated for brevity)",
		},
		{
			name:     "exact length unchanged",
			diff:     "exact",
			maxLen:   5,
			expected: "exact",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateDiff(tt.diff, tt.maxLen)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestSCMShortcut_GetPRPlanSystemPrompt(t *testing.T) {
	cfg := config.DefaultConfig()
	shortcut := NewSCMShortcut(nil, cfg, nil)

	prompt := shortcut.getPRPlanSystemPrompt()

	if prompt == "" {
		t.Error("Expected non-empty system prompt")
	}

	expectedPhrases := []string{
		"Branch:",
		"Commit:",
		"PR Title:",
		"PR Description:",
	}

	for _, phrase := range expectedPhrases {
		if !contains(prompt, phrase) {
			t.Errorf("Expected system prompt to contain %q", phrase)
		}
	}
}

func TestSCMShortcut_BuildPRPlanUserPrompt_FeatureBranch(t *testing.T) {
	cfg := config.DefaultConfig()
	shortcut := NewSCMShortcut(nil, cfg, nil)

	prompt := shortcut.buildPRPlanUserPrompt("diff content", "feature-branch", false, "main")

	expectedPhrases := []string{
		"feature-branch",
		"Already on a feature branch",
		"diff content",
		"Base branch: main",
	}

	for _, phrase := range expectedPhrases {
		if !contains(prompt, phrase) {
			t.Errorf("Expected prompt to contain %q", phrase)
		}
	}
}

func TestSCMShortcut_BuildPRPlanUserPrompt_MainBranch(t *testing.T) {
	cfg := config.DefaultConfig()
	shortcut := NewSCMShortcut(nil, cfg, nil)

	prompt := shortcut.buildPRPlanUserPrompt("diff content", "main", true, "main")

	if !contains(prompt, "will create a new feature branch") {
		t.Error("Expected prompt to indicate need for new branch when on main")
	}
}

func TestSCMShortcut_FormatIssuesList(t *testing.T) {
	cfg := config.DefaultConfig()
	shortcut := NewSCMShortcut(nil, cfg, nil)

	jsonOutput := `[{"number":1,"title":"Test Issue"}]`
	formatted := shortcut.formatIssuesList(jsonOutput)

	expectedPhrases := []string{
		"GitHub Issues",
		jsonOutput,
		"/scm issues <number>",
	}

	for _, phrase := range expectedPhrases {
		if !contains(formatted, phrase) {
			t.Errorf("Expected formatted output to contain %q", phrase)
		}
	}
}

func TestSCMShortcut_FormatIssueDetails(t *testing.T) {
	cfg := config.DefaultConfig()
	shortcut := NewSCMShortcut(nil, cfg, nil)

	jsonOutput := `{"number":1,"title":"Test Issue","body":"Description"}`
	formatted := shortcut.formatIssueDetails(jsonOutput)

	expectedPhrases := []string{
		"GitHub Issue Details",
		jsonOutput,
	}

	for _, phrase := range expectedPhrases {
		if !contains(formatted, phrase) {
			t.Errorf("Expected formatted output to contain %q", phrase)
		}
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
