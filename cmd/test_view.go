package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/ui"
	"github.com/inference-gateway/cli/internal/ui/components"
	"github.com/inference-gateway/cli/internal/ui/shared"
	"github.com/spf13/cobra"
)

var testViewCmd = &cobra.Command{
	Use:   "test-view [component]",
	Short: "Test and preview UI components directly in terminal",
	Long: `Test and preview UI components directly in terminal for faster iteration.
Available components:
  approval   - Test the tool approval view with sample data
  diff       - Test the diff renderer with sample content
  large-file - Test large file editing/writing for screen responsiveness`,
	Args: cobra.MaximumNArgs(1),
	Run:  runTestView,
}

var (
	sampleOldContent string
	sampleNewContent string
	componentWidth   int
	componentHeight  int
)

func runTestView(cmd *cobra.Command, args []string) {
	component := "approval"
	if len(args) > 0 {
		component = args[0]
	}

	theme := createTestTheme()

	switch component {
	case "approval":
		testApprovalView(theme)
	case "diff":
		testDiffRenderer(theme)
	case "large-file":
		testLargeFileView(theme)
	default:
		fmt.Printf("Unknown component: %s\n", component)
		fmt.Println("Available components: approval, diff, large-file")
		os.Exit(1)
	}
}

func createTestTheme() shared.Theme {
	return ui.NewDefaultTheme()
}

func testApprovalView(theme shared.Theme) {
	fmt.Println("🔧 Testing Approval View Component")
	fmt.Println(shared.CreateSeparator(50, "─"))

	approvalComponent := components.NewApprovalComponent(theme)
	approvalComponent.SetWidth(componentWidth)
	approvalComponent.SetHeight(componentHeight)

	// Create sample tool execution session
	toolCall := &domain.ToolCall{
		Name: "Edit",
		Arguments: map[string]interface{}{
			"file_path":   "internal/ui/components/approval_view.go",
			"old_string":  "func (a *ApprovalComponent) View() string {\n\treturn \"\"\n}",
			"new_string":  "func (a *ApprovalComponent) View() string {\n\treturn a.Render(nil, 0)\n}",
			"replace_all": false,
		},
	}

	toolExecution := &domain.ToolExecutionSession{
		RequiresApproval: true,
		CurrentTool:      toolCall,
	}

	// Test with different formatter if Edit tool needs special formatting
	diffRenderer := components.NewDiffRenderer(theme)
	toolFormatter := &MockToolFormatter{diffRenderer: diffRenderer}
	approvalComponent.SetToolFormatter(toolFormatter)

	// Test with different selected indices
	selectedIndices := []int{0, 1}
	for i, selectedIndex := range selectedIndices {
		fmt.Printf("\n--- Test Case %d: Selected Index %d ---\n", i+1, selectedIndex)
		output := approvalComponent.Render(toolExecution, selectedIndex)
		fmt.Println(output)
	}
}

func testDiffRenderer(theme shared.Theme) {
	fmt.Println("🎨 Testing Diff Renderer Component")
	fmt.Println(shared.CreateSeparator(50, "─"))

	diffRenderer := components.NewDiffRenderer(theme)

	// Test 1: Edit tool arguments
	fmt.Println("\n--- Test Case 1: Edit Tool Arguments ---")
	editArgs := map[string]any{
		"file_path":   "internal/ui/components/approval_view.go",
		"old_string":  sampleOldContent,
		"new_string":  sampleNewContent,
		"replace_all": false,
	}
	editOutput := diffRenderer.RenderEditToolArguments(editArgs)
	fmt.Println(editOutput)

	// Test 2: MultiEdit tool arguments
	fmt.Println("\n--- Test Case 2: MultiEdit Tool Arguments ---")
	multiEditArgs := map[string]any{
		"file_path": "internal/ui/components/test_component.go",
		"edits": []any{
			map[string]any{
				"old_string":  "package components",
				"new_string":  "package ui_components",
				"replace_all": false,
			},
			map[string]any{
				"old_string":  "func Test()",
				"new_string":  "func TestComponent()",
				"replace_all": false,
			},
		},
	}
	multiEditOutput := diffRenderer.RenderMultiEditToolArguments(multiEditArgs)
	fmt.Println(multiEditOutput)

	// Test 3: Pure diff rendering
	fmt.Println("\n--- Test Case 3: Pure Diff Rendering ---")
	diffOutput := diffRenderer.RenderColoredDiff(sampleOldContent, sampleNewContent)
	fmt.Println(diffOutput)
}

func testLargeFileView(theme shared.Theme) {
	fmt.Println("📁 Testing Large File Edit/Write View Component")
	fmt.Println(shared.CreateSeparator(80, "─"))

	// Generate large content to simulate real-world scenarios
	largeOldContent := generateLargeFileContent("OLD", 100)
	largeNewContent := generateLargeFileContent("NEW", 100)

	// Test different screen sizes
	screenSizes := []struct {
		width  int
		height int
		name   string
	}{
		{80, 24, "Small Terminal (80x24)"},
		{120, 40, "Medium Terminal (120x40)"},
		{160, 50, "Large Terminal (160x50)"},
		{200, 60, "Extra Large Terminal (200x60)"},
	}

	for _, size := range screenSizes {
		fmt.Printf("\n=== %s ===\n", size.name)
		fmt.Printf("Testing responsiveness at %dx%d\n", size.width, size.height)
		fmt.Println(shared.CreateSeparator(size.width/2, "─"))

		// Test approval component with large file
		approvalComponent := components.NewApprovalComponent(theme)
		approvalComponent.SetWidth(size.width)
		approvalComponent.SetHeight(size.height)

		toolCall := &domain.ToolCall{
			Name: "Edit",
			Arguments: map[string]interface{}{
				"file_path":   "test/large_file.go",
				"old_string":  largeOldContent,
				"new_string":  largeNewContent,
				"replace_all": false,
			},
		}

		toolExecution := &domain.ToolExecutionSession{
			RequiresApproval: true,
			CurrentTool:      toolCall,
		}

		diffRenderer := components.NewDiffRenderer(theme)
		toolFormatter := &MockToolFormatter{diffRenderer: diffRenderer}
		approvalComponent.SetToolFormatter(toolFormatter)

		// Test rendering at this screen size
		output := approvalComponent.Render(toolExecution, 0)
		lines := len(strings.Split(output, "\n"))

		fmt.Printf("Rendered output has %d lines (fits in %d height: %t)\n",
			lines, size.height, lines <= size.height)

		// Show truncated preview
		previewLines := strings.Split(output, "\n")
		maxPreview := 5
		if len(previewLines) > maxPreview {
			for i := 0; i < maxPreview; i++ {
				fmt.Println(previewLines[i])
			}
			fmt.Printf("... (%d more lines)\n", len(previewLines)-maxPreview)
		} else {
			fmt.Println(output)
		}

		// Test diff renderer directly
		fmt.Printf("\n--- Direct Diff Renderer Test at %dx%d ---\n", size.width, size.height)
		diffOutput := diffRenderer.RenderColoredDiff(largeOldContent[:200], largeNewContent[:200])
		diffLines := strings.Split(diffOutput, "\n")
		fmt.Printf("Diff output has %d lines\n", len(diffLines))

		// Show first few lines of diff
		for i := 0; i < min(3, len(diffLines)); i++ {
			fmt.Println(diffLines[i])
		}
		if len(diffLines) > 3 {
			fmt.Printf("... (%d more lines)\n", len(diffLines)-3)
		}
	}

	// Test Write tool with large content
	fmt.Printf("\n=== Testing Write Tool with Large Content ===\n")
	fmt.Println(shared.CreateSeparator(80, "─"))

	writeToolCall := &domain.ToolCall{
		Name: "Write",
		Arguments: map[string]interface{}{
			"file_path": "test/new_large_file.go",
			"content":   generateLargeFileContent("WRITE", 150),
		},
	}

	writeToolExecution := &domain.ToolExecutionSession{
		RequiresApproval: true,
		CurrentTool:      writeToolCall,
	}

	approvalComponent := components.NewApprovalComponent(theme)
	approvalComponent.SetWidth(120)
	approvalComponent.SetHeight(30)

	diffRenderer := components.NewDiffRenderer(theme)
	toolFormatter := &MockToolFormatter{diffRenderer: diffRenderer}
	approvalComponent.SetToolFormatter(toolFormatter)

	writeOutput := approvalComponent.Render(writeToolExecution, 0)
	writeLines := strings.Split(writeOutput, "\n")

	fmt.Printf("Write tool approval renders %d lines\n", len(writeLines))

	// Show preview
	for i := 0; i < min(10, len(writeLines)); i++ {
		fmt.Println(writeLines[i])
	}
	if len(writeLines) > 10 {
		fmt.Printf("... (%d more lines)\n", len(writeLines)-10)
	}
}

func generateLargeFileContent(prefix string, lines int) string {
	var content strings.Builder

	content.WriteString("package main\n\n")
	content.WriteString("import (\n")
	content.WriteString("\t\"fmt\"\n")
	content.WriteString("\t\"os\"\n")
	content.WriteString("\t\"strings\"\n")
	content.WriteString(")\n\n")

	for i := 0; i < lines; i++ {
		content.WriteString(fmt.Sprintf("// %s Content Line %d with some additional text to make it longer\n", prefix, i+1))
		content.WriteString(fmt.Sprintf("func %sFunction%d() {\n", strings.ToLower(prefix), i+1))
		content.WriteString(fmt.Sprintf("\tfmt.Println(\"This is %s function %d with some sample content\")\n", prefix, i+1))
		content.WriteString("\t// Add some logic here\n")
		content.WriteString("\tif true {\n")
		content.WriteString("\t\tfmt.Println(\"Nested condition for testing purposes\")\n")
		content.WriteString("\t}\n")
		content.WriteString("}\n\n")
	}

	return content.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// MockToolFormatter implements domain.ToolFormatter for testing
type MockToolFormatter struct {
	diffRenderer *components.DiffRenderer
}

func (m *MockToolFormatter) FormatToolCall(toolName string, args map[string]any) string {
	return fmt.Sprintf("%s(args...)", toolName)
}

func (m *MockToolFormatter) FormatToolArgumentsForApproval(toolName string, args map[string]interface{}) string {
	switch toolName {
	case "Edit":
		return m.diffRenderer.RenderEditToolArguments(args)
	case "MultiEdit":
		return m.diffRenderer.RenderMultiEditToolArguments(args)
	case "Write":
		// For Write tools, show a vim-like preview format
		if content, ok := args["content"].(string); ok {
			return m.renderWriteContentPreview(content, args)
		}
		return fmt.Sprintf("Mock formatting for %s with args: %v", toolName, args)
	default:
		return fmt.Sprintf("Mock formatting for %s with args: %v", toolName, args)
	}
}

func (m *MockToolFormatter) renderWriteContentPreview(content string, args map[string]interface{}) string {
	var preview strings.Builder

	// Add file path header
	if filePath, ok := args["file_path"].(string); ok {
		preview.WriteString(fmt.Sprintf("File: %s\n", filePath))
		preview.WriteString(strings.Repeat("─", 50) + "\n")
	}

	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	// Show first 15 lines with vim-like line numbers
	maxPreview := min(15, totalLines)
	for i := 0; i < maxPreview; i++ {
		lineNum := fmt.Sprintf("%4d", i+1)
		preview.WriteString(fmt.Sprintf("%s │ %s\n", lineNum, lines[i]))
	}

	// Add summary if content is longer
	if totalLines > maxPreview {
		preview.WriteString(fmt.Sprintf("... (%d more lines)\n", totalLines-maxPreview))
		preview.WriteString(strings.Repeat("─", 50) + "\n")
		preview.WriteString(fmt.Sprintf("Total: %d lines, %d characters", totalLines, len(content)))
	}

	return preview.String()
}

func (m *MockToolFormatter) FormatToolResultForUI(result *domain.ToolExecutionResult, terminalWidth int) string {
	return "Mock tool result for UI"
}

func (m *MockToolFormatter) FormatToolResultExpanded(result *domain.ToolExecutionResult, terminalWidth int) string {
	return "Mock expanded tool result"
}

func (m *MockToolFormatter) FormatToolResultForLLM(result *domain.ToolExecutionResult) string {
	return "Mock tool result for LLM"
}

func (m *MockToolFormatter) ShouldAlwaysExpandTool(toolName string) bool {
	return false
}

func init() {
	testViewCmd.Flags().StringVar(&sampleOldContent, "old",
		"func (a *ApprovalComponent) View() string {\n\treturn \"\"\n}",
		"Sample old content for diff testing")
	testViewCmd.Flags().StringVar(&sampleNewContent, "new",
		"func (a *ApprovalComponent) View() string {\n\treturn a.Render(nil, 0)\n}",
		"Sample new content for diff testing")
	testViewCmd.Flags().IntVar(&componentWidth, "width", 100, "Component width for testing")
	testViewCmd.Flags().IntVar(&componentHeight, "height", 30, "Component height for testing")

	rootCmd.AddCommand(testViewCmd)
}
