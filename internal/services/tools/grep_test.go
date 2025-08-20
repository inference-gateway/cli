package tools

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/inference-gateway/cli/config"
)

func TestGrepTool_Definition(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Grep: config.GrepToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewGrepTool(cfg)
	def := tool.Definition()

	if def.Name != "Grep" {
		t.Errorf("Expected tool name to be 'Grep', got %s", def.Name)
	}

	if def.Description == "" {
		t.Error("Expected non-empty description")
	}

	expectedPhrases := []string{
		"ALWAYS use Grep for search tasks",
		"configurable backend",
		"Output modes",
		"files_with_matches",
		"content",
		"count",
	}

	for _, phrase := range expectedPhrases {
		if !contains(def.Description, phrase) {
			t.Errorf("Expected description to contain '%s'", phrase)
		}
	}

	params, ok := def.Parameters.(map[string]interface{})
	if !ok {
		t.Fatal("Expected parameters to be a map")
	}

	properties, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected properties to be a map")
	}

	required, ok := params["required"].([]string)
	if !ok {
		t.Fatal("Expected required to be a string slice")
	}

	if len(required) != 1 || required[0] != "pattern" {
		t.Errorf("Expected required to be ['pattern'], got %v", required)
	}

	essentialParams := []string{"pattern", "output_mode", "glob", "type", "-i", "-n", "-A", "-B", "-C", "multiline", "head_limit"}
	for _, param := range essentialParams {
		if _, exists := properties[param]; !exists {
			t.Errorf("Expected parameter '%s' to exist", param)
		}
	}
}

func TestGrepTool_IsEnabled(t *testing.T) {
	tests := []struct {
		name           string
		toolsEnabled   bool
		grepEnabled    bool
		expectedResult bool
	}{
		{"both enabled", true, true, true},
		{"tools disabled", false, true, false},
		{"grep disabled", true, false, false},
		{"both disabled", false, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Tools: config.ToolsConfig{
					Enabled: tt.toolsEnabled,
					Grep: config.GrepToolConfig{
						Enabled: tt.grepEnabled,
					},
				},
			}

			tool := NewGrepTool(cfg)
			result := tool.IsEnabled()

			if result != tt.expectedResult {
				t.Errorf("Expected IsEnabled() to be %v, got %v", tt.expectedResult, result)
			}
		})
	}
}

func TestGrepTool_Validate(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Grep: config.GrepToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewGrepTool(cfg)

	t.Run("pattern validation", func(t *testing.T) {
		testGrepValidationCases(t, tool, getPatternTestCases())
	})

	t.Run("output mode validation", func(t *testing.T) {
		testGrepValidationCases(t, tool, getOutputModeTestCases())
	})

	t.Run("context flags validation", func(t *testing.T) {
		testGrepValidationCases(t, tool, getContextFlagsTestCases())
	})

	t.Run("head limit validation", func(t *testing.T) {
		testGrepValidationCases(t, tool, getHeadLimitTestCases())
	})

	t.Run("boolean flags validation", func(t *testing.T) {
		testGrepValidationCases(t, tool, getBooleanFlagsTestCases())
	})
}

func testGrepValidationCases(t *testing.T, tool *GrepTool, tests []grepValidationTestCase) {
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			validateGrepTestResult(t, err, tt.expectError, tt.errorMsg)
		})
	}
}

func validateGrepTestResult(t *testing.T, err error, expectError bool, errorMsg string) {
	if expectError {
		if err == nil {
			t.Errorf("Expected error containing '%s', got nil", errorMsg)
			return
		}
		if !contains(err.Error(), errorMsg) {
			t.Errorf("Expected error containing '%s', got '%s'", errorMsg, err.Error())
		}
		return
	}

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
}

type grepValidationTestCase struct {
	name        string
	args        map[string]interface{}
	expectError bool
	errorMsg    string
}

func getPatternTestCases() []grepValidationTestCase {
	return []grepValidationTestCase{
		{
			name:        "missing pattern",
			args:        map[string]interface{}{},
			expectError: true,
			errorMsg:    "pattern parameter is required",
		},
		{
			name: "empty pattern",
			args: map[string]interface{}{
				"pattern": "",
			},
			expectError: true,
			errorMsg:    "pattern cannot be empty",
		},
		{
			name: "invalid regex pattern",
			args: map[string]interface{}{
				"pattern": "[",
			},
			expectError: true,
			errorMsg:    "invalid regex pattern",
		},
		{
			name: "valid pattern",
			args: map[string]interface{}{
				"pattern": "test.*pattern",
			},
			expectError: false,
		},
	}
}

func getOutputModeTestCases() []grepValidationTestCase {
	return []grepValidationTestCase{
		{
			name: "invalid output_mode",
			args: map[string]interface{}{
				"pattern":     "test",
				"output_mode": "invalid",
			},
			expectError: true,
			errorMsg:    "invalid output_mode",
		},
		{
			name: "valid output_mode content",
			args: map[string]interface{}{
				"pattern":     "test",
				"output_mode": "content",
			},
			expectError: false,
		},
		{
			name: "valid output_mode files_with_matches",
			args: map[string]interface{}{
				"pattern":     "test",
				"output_mode": "files_with_matches",
			},
			expectError: false,
		},
		{
			name: "valid output_mode count",
			args: map[string]interface{}{
				"pattern":     "test",
				"output_mode": "count",
			},
			expectError: false,
		},
	}
}

func getContextFlagsTestCases() []grepValidationTestCase {
	return []grepValidationTestCase{
		{
			name: "negative context value",
			args: map[string]interface{}{
				"pattern": "test",
				"-A":      -1.0,
			},
			expectError: true,
			errorMsg:    "-A must be >= 0",
		},
		{
			name: "invalid context type",
			args: map[string]interface{}{
				"pattern": "test",
				"-B":      "invalid",
			},
			expectError: true,
			errorMsg:    "-B must be a number",
		},
		{
			name: "valid context values",
			args: map[string]interface{}{
				"pattern": "test",
				"-A":      3.0,
				"-B":      2.0,
				"-C":      1.0,
			},
			expectError: false,
		},
	}
}

func getHeadLimitTestCases() []grepValidationTestCase {
	return []grepValidationTestCase{
		{
			name: "invalid head_limit",
			args: map[string]interface{}{
				"pattern":    "test",
				"head_limit": 0.0,
			},
			expectError: true,
			errorMsg:    "head_limit must be > 0",
		},
		{
			name: "valid head_limit",
			args: map[string]interface{}{
				"pattern":    "test",
				"head_limit": 10.0,
			},
			expectError: false,
		},
	}
}

func getBooleanFlagsTestCases() []grepValidationTestCase {
	return []grepValidationTestCase{
		{
			name: "invalid boolean type",
			args: map[string]interface{}{
				"pattern": "test",
				"-i":      "not_boolean",
			},
			expectError: true,
			errorMsg:    "-i must be a boolean",
		},
		{
			name: "valid boolean flags",
			args: map[string]interface{}{
				"pattern":   "test",
				"-i":        true,
				"-n":        false,
				"multiline": true,
			},
			expectError: false,
		},
	}
}

func TestGrepTool_ValidateDisabled(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: false,
		},
	}

	tool := NewGrepTool(cfg)
	args := map[string]interface{}{
		"pattern": "test",
	}

	err := tool.Validate(args)
	if err == nil {
		t.Error("Expected error when grep tool is disabled")
	}

	if !contains(err.Error(), "grep tool is not enabled") {
		t.Errorf("Expected error about tool being disabled, got: %v", err)
	}
}

func TestGrepTool_Execute(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Grep: config.GrepToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewGrepTool(cfg)
	ctx := context.Background()

	t.Run("missing pattern", func(t *testing.T) {
		args := map[string]interface{}{}
		result, err := tool.Execute(ctx, args)

		if err != nil {
			t.Errorf("Expected no error from Execute, got: %v", err)
		}

		if result.Success {
			t.Error("Expected result.Success to be false")
		}

		if !contains(result.Error, "pattern parameter is required") {
			t.Errorf("Expected error about missing pattern, got: %s", result.Error)
		}
	})

	t.Run("disabled tool", func(t *testing.T) {
		disabledCfg := &config.Config{
			Tools: config.ToolsConfig{
				Enabled: false,
			},
		}
		disabledTool := NewGrepTool(disabledCfg)

		args := map[string]interface{}{
			"pattern": "test",
		}

		_, err := disabledTool.Execute(ctx, args)

		if err == nil {
			t.Error("Expected error when tool is disabled")
		}

		if !contains(err.Error(), "grep tool is not enabled") {
			t.Errorf("Expected error about tool being disabled, got: %v", err)
		}
	})
}

func TestGrepTool_PathExclusion(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{"."},
			},
			Grep: config.GrepToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewGrepTool(cfg)

	tests := []struct {
		name     string
		path     string
		excluded bool
	}{
		{"git directory", ".git/", true},
		{"git file", ".git/config", true},
		{"infer directory", ".infer/", true},
		{"infer file", ".infer/config.yaml", true},
		{"secret directory", "secret/", true},
		{"secret file", "secret/key.txt", true},
		{"allowed directory", "src/", false},
		{"allowed file", "main.go", false},
		{"current directory", ".", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tool.isPathExcluded(tt.path)
			if result != tt.excluded {
				t.Errorf("Expected isPathExcluded(%s) to be %v, got %v", tt.path, tt.excluded, result)
			}
		})
	}
}

func TestGrepResult_Structure(t *testing.T) {
	result := &GrepResult{
		Pattern:    "test.*pattern",
		OutputMode: "files_with_matches",
		Files:      []string{"file1.go", "file2.go"},
		Total:      2,
		Truncated:  false,
		Duration:   "1.5ms",
	}

	if result.Pattern != "test.*pattern" {
		t.Errorf("Expected pattern to be 'test.*pattern', got %s", result.Pattern)
	}

	if result.OutputMode != "files_with_matches" {
		t.Errorf("Expected output mode to be 'files_with_matches', got %s", result.OutputMode)
	}

	if len(result.Files) != 2 {
		t.Errorf("Expected 2 files, got %d", len(result.Files))
	}

	if result.Total != 2 {
		t.Errorf("Expected total to be 2, got %d", result.Total)
	}

	if result.Truncated != false {
		t.Errorf("Expected truncated to be false, got %v", result.Truncated)
	}
}

func TestGrepMatch_Structure(t *testing.T) {
	match := GrepMatch{
		File: "main.go",
		Line: 42,
		Text: "func main() {",
	}

	if match.File != "main.go" {
		t.Errorf("Expected file to be 'main.go', got %s", match.File)
	}

	if match.Line != 42 {
		t.Errorf("Expected line to be 42, got %d", match.Line)
	}

	if match.Text != "func main() {" {
		t.Errorf("Expected text to be 'func main() {', got %s", match.Text)
	}
}

func TestGrepCount_Structure(t *testing.T) {
	count := GrepCount{
		File:  "test.go",
		Count: 5,
	}

	if count.File != "test.go" {
		t.Errorf("Expected file to be 'test.go', got %s", count.File)
	}

	if count.Count != 5 {
		t.Errorf("Expected count to be 5, got %d", count.Count)
	}
}

func TestGrepTool_RipgrepDetection(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Grep: config.GrepToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewGrepTool(cfg)

	if !tool.useRipgrep {
		t.Error("Expected ripgrep to be detected and enabled")
	}

	if tool.ripgrepPath == "" {
		t.Error("Expected ripgrep path to be set")
	}

	t.Logf("Ripgrep detected at: %s", tool.ripgrepPath)
}

func TestGrepTool_HybridSearch(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{"."},
			},
			Grep: config.GrepToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewGrepTool(cfg)
	ctx := context.Background()

	args := map[string]interface{}{
		"pattern":     "package",
		"output_mode": "files_with_matches",
	}

	result, err := tool.Execute(ctx, args)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if !result.Success {
		t.Errorf("Expected successful execution")
	}

	if result.ToolName != "Grep" {
		t.Errorf("Expected tool name to be 'Grep', got %s", result.ToolName)
	}

	grepResult, ok := result.Data.(*GrepResult)
	if !ok {
		t.Fatal("Expected GrepResult data")
	}

	if len(grepResult.Files) == 0 {
		t.Error("Expected to find files with 'package' keyword")
	}

	t.Logf("Found %d files with 'package' keyword using %s",
		len(grepResult.Files),
		map[bool]string{true: "ripgrep", false: "Go implementation"}[tool.useRipgrep])
}

func TestGrepTool_GoBasedSearch(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	tool := &GrepTool{
		config: &config.Config{
			Tools: config.ToolsConfig{
				Enabled: true,
				Sandbox: config.SandboxConfig{
					Directories: []string{wd},
				},
			},
		},
		enabled: true,
	}

	ctx := context.Background()
	args := map[string]interface{}{
		"pattern":     "package",
		"output_mode": "files_with_matches",
	}

	result, err := tool.Execute(ctx, args)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if !result.Success {
		t.Errorf("Expected successful execution")
	}

	if result.ToolName != "Grep" {
		t.Errorf("Expected tool name to be 'Grep', got %s", result.ToolName)
	}
}

func BenchmarkGrepTool_SimplePattern(b *testing.B) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Grep: config.GrepToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewGrepTool(cfg)
	ctx := context.Background()
	args := map[string]interface{}{
		"pattern":     "func",
		"output_mode": "files_with_matches",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tool.Execute(ctx, args)
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
	}
}

func BenchmarkGrepTool_ComplexRegex(b *testing.B) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Grep: config.GrepToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewGrepTool(cfg)
	ctx := context.Background()
	args := map[string]interface{}{
		"pattern":     `func\s+\w+\s*\([^)]*\)\s*\{`,
		"output_mode": "content",
		"-n":          true,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tool.Execute(ctx, args)
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
	}
}

func BenchmarkGrepTool_FileTypeFilter(b *testing.B) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Grep: config.GrepToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewGrepTool(cfg)
	ctx := context.Background()
	args := map[string]interface{}{
		"pattern":     "import",
		"type":        "go",
		"output_mode": "files_with_matches",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tool.Execute(ctx, args)
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
	}
}

func BenchmarkGrepTool_GlobPattern(b *testing.B) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Grep: config.GrepToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewGrepTool(cfg)
	ctx := context.Background()
	args := map[string]interface{}{
		"pattern":     "package",
		"glob":        "*.go",
		"output_mode": "count",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tool.Execute(ctx, args)
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
	}
}

func BenchmarkGrepTool_CaseInsensitive(b *testing.B) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Grep: config.GrepToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewGrepTool(cfg)
	ctx := context.Background()
	args := map[string]interface{}{
		"pattern":     "FUNC",
		"-i":          true,
		"output_mode": "files_with_matches",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tool.Execute(ctx, args)
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
	}
}

func BenchmarkGrepTool_WithContext(b *testing.B) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Grep: config.GrepToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewGrepTool(cfg)
	ctx := context.Background()
	args := map[string]interface{}{
		"pattern":     "func",
		"output_mode": "content",
		"-n":          true,
		"-C":          2.0,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tool.Execute(ctx, args)
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
	}
}

func BenchmarkGrepTool_HeadLimit(b *testing.B) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Grep: config.GrepToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewGrepTool(cfg)
	ctx := context.Background()
	args := map[string]interface{}{
		"pattern":     ".",
		"output_mode": "files_with_matches",
		"head_limit":  10.0,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tool.Execute(ctx, args)
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
	}
}

func BenchmarkRipgrepComparison_SimplePattern(b *testing.B) {
	b.Run("GrepTool", func(b *testing.B) {
		cfg := &config.Config{
			Tools: config.ToolsConfig{
				Enabled: true,
				Grep: config.GrepToolConfig{
					Enabled: true,
				},
			},
		}

		tool := NewGrepTool(cfg)
		ctx := context.Background()
		args := map[string]interface{}{
			"pattern":     "func",
			"output_mode": "files_with_matches",
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := tool.Execute(ctx, args)
			if err != nil {
				b.Fatalf("Execute failed: %v", err)
			}
		}
	})

	b.Run("RipgrepNative", func(b *testing.B) {
		rgPath, err := exec.LookPath("rg")
		if err != nil {
			b.Skip("ripgrep not found in PATH, skipping native comparison")
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			cmd := exec.Command(rgPath, "--files-with-matches", "func", ".")
			_, err := cmd.Output()
			if err != nil {
				b.Fatalf("rg command failed: %v", err)
			}
		}
	})
}

func BenchmarkRipgrepComparison_ComplexRegex(b *testing.B) {
	b.Run("GrepTool", func(b *testing.B) {
		cfg := &config.Config{
			Tools: config.ToolsConfig{
				Enabled: true,
				Grep: config.GrepToolConfig{
					Enabled: true,
				},
			},
		}

		tool := NewGrepTool(cfg)
		ctx := context.Background()
		args := map[string]interface{}{
			"pattern":     `func\s+\w+\s*\([^)]*\)\s*\{`,
			"output_mode": "content",
			"-n":          true,
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := tool.Execute(ctx, args)
			if err != nil {
				b.Fatalf("Execute failed: %v", err)
			}
		}
	})

	b.Run("RipgrepNative", func(b *testing.B) {
		rgPath, err := exec.LookPath("rg")
		if err != nil {
			b.Skip("ripgrep not found in PATH, skipping native comparison")
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			cmd := exec.Command(rgPath, "-n", `func\s+\w+\s*\([^)]*\)\s*\{`, ".")
			_, err := cmd.Output()
			if err != nil {
				b.Fatalf("rg command failed: %v", err)
			}
		}
	})
}

func BenchmarkRipgrepComparison_FileTypeFilter(b *testing.B) {
	b.Run("GrepTool", func(b *testing.B) {
		cfg := &config.Config{
			Tools: config.ToolsConfig{
				Enabled: true,
				Grep: config.GrepToolConfig{
					Enabled: true,
				},
			},
		}

		tool := NewGrepTool(cfg)
		ctx := context.Background()
		args := map[string]interface{}{
			"pattern":     "import",
			"type":        "go",
			"output_mode": "files_with_matches",
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := tool.Execute(ctx, args)
			if err != nil {
				b.Fatalf("Execute failed: %v", err)
			}
		}
	})

	b.Run("RipgrepNative", func(b *testing.B) {
		rgPath, err := exec.LookPath("rg")
		if err != nil {
			b.Skip("ripgrep not found in PATH, skipping native comparison")
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			cmd := exec.Command(rgPath, "--files-with-matches", "--type", "go", "import", ".")
			_, err := cmd.Output()
			if err != nil {
				b.Fatalf("rg command failed: %v", err)
			}
		}
	})
}

func BenchmarkGrepTool_MemoryAllocs(b *testing.B) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Grep: config.GrepToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewGrepTool(cfg)
	ctx := context.Background()
	args := map[string]interface{}{
		"pattern":     "func",
		"output_mode": "files_with_matches",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tool.Execute(ctx, args)
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			(len(s) > len(substr) &&
				(s[:len(substr)] == substr ||
					s[len(s)-len(substr):] == substr ||
					strings.Contains(s, substr))))
}
