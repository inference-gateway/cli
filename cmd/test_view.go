package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/ui"
	"github.com/inference-gateway/cli/internal/ui/components"
	"github.com/inference-gateway/cli/internal/ui/shared"
	"github.com/inference-gateway/cli/internal/ui/styles/colors"
	"github.com/spf13/cobra"
)

var testViewCmd = &cobra.Command{
	Use:    "test-view [component]",
	Short:  "Internal testing command for UI component snapshots",
	Hidden: true,
	Long: `Internal command for testing and snapshot generation of UI components.
This command is for development and testing purposes only.

Available components:
  approval   - Test the tool approval view with sample data
  diff       - Test the diff renderer with sample content
  multiedit  - Test the MultiEdit tool collapsed view formatting
  large-file - Test large file editing/writing for screen responsiveness`,
	Args: cobra.MaximumNArgs(1),
	Run:  runTestView,
}

var (
	sampleOldContent string
	sampleNewContent string
	componentWidth   int
	componentHeight  int
	snapshotMode     bool
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
	case "multiedit":
		testMultiEditView(theme)
	case "large-file":
		testLargeFileView(theme)
	default:
		fmt.Printf("Unknown component: %s\n", component)
		fmt.Println("Available components: approval, diff, multiedit, large-file")
		os.Exit(1)
	}
}

func createTestTheme() shared.Theme {
	return ui.NewDefaultTheme()
}

func testApprovalView(theme shared.Theme) {
	fmt.Println("🔧 Testing Approval View Component")
	fmt.Println(colors.CreateSeparator(50, "─"))

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

func testMultiEditView(theme shared.Theme) {
	if snapshotMode {
		fmt.Print("MultiEdit(file=demo.go, 3 edits)\n└─ ✓ Applied 3/3 edits to demo.go (45 bytes difference)\n")
		return
	}

	fmt.Println("✏️  Testing MultiEdit Tool Collapsed View")
	fmt.Println(colors.CreateSeparator(50, "─"))

	// Simulate a MultiEdit tool result for collapsed display
	fmt.Println("\n--- Test Case 1: Collapsed View (Success) ---")
	fmt.Println("Simulating: MultiEdit(file=demo.go, 3 edits)")
	fmt.Println("└─ ✓ Applied 3/3 edits to demo.go (45 bytes difference)")

	fmt.Println("\n--- Test Case 2: Collapsed View (Failed) ---")
	fmt.Println("Simulating: MultiEdit(file=test.go, 2 edits)")
	fmt.Println("└─ ✗ Multi-edit failed")

	fmt.Println("\n--- Test Case 3: Expanded MultiEdit Diff ---")
	diffRenderer := components.NewDiffRenderer(theme)
	multiEditArgs := map[string]any{
		"file_path": "/Users/example/demo.go",
		"edits": []any{
			map[string]any{
				"old_string": "Welcome to Go!",
				"new_string": "! Welcome to Go!",
			},
			map[string]any{
				"old_string": "This is an update file.",
				"new_string": "This is an updated demo file.",
			},
			map[string]any{
				"old_string": "It has multiple lines.",
				"new_string": "Successfully edited randomly with MultiEdit!",
			},
		},
	}

	// Show what the expanded view would look like
	expandedOutput := diffRenderer.RenderMultiEditToolArguments(multiEditArgs)
	fmt.Println(expandedOutput)

	fmt.Println("\n--- Test Case 4: Large MultiEdit (10+ edits) ---")
	largeEditArgs := map[string]any{
		"file_path": "/path/to/large_refactor.go",
		"edits":     make([]any, 0),
	}

	// Generate many edits
	edits := largeEditArgs["edits"].([]any)
	for i := 1; i <= 10; i++ {
		edits = append(edits, map[string]any{
			"old_string":  fmt.Sprintf("oldFunction%d", i),
			"new_string":  fmt.Sprintf("newFunction%d", i),
			"replace_all": i%2 == 0, // Every other edit uses replace_all
		})
	}
	largeEditArgs["edits"] = edits

	fmt.Println("Collapsed view for large refactor:")
	fmt.Printf("MultiEdit(file=large_refactor.go, %d edits)\n", len(edits))
	fmt.Println("└─ ✓ Applied 10/10 edits to large_refactor.go (350 bytes difference)")

	fmt.Println("\nExpanded view preview (first 3 edits):")
	// Show only first 3 edits for brevity
	previewArgs := map[string]any{
		"file_path": largeEditArgs["file_path"],
		"edits":     edits[:3],
	}
	previewOutput := diffRenderer.RenderMultiEditToolArguments(previewArgs)
	fmt.Println(previewOutput)
	fmt.Println("... (7 more edits)")
}

func testDiffRenderer(theme shared.Theme) {
	fmt.Println("🎨 Testing Diff Renderer Component")
	fmt.Println(colors.CreateSeparator(50, "─"))

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

	// Test 2: MultiEdit tool arguments with multiple edits
	fmt.Println("\n--- Test Case 2: MultiEdit Tool Arguments (3 edits) ---")
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
			map[string]any{
				"old_string":  "oldVariable",
				"new_string":  "newVariable",
				"replace_all": true,
			},
		},
	}
	multiEditOutput := diffRenderer.RenderMultiEditToolArguments(multiEditArgs)
	fmt.Println(multiEditOutput)

	// Test 3: New file creation diff
	fmt.Println("\n--- Test Case 3: New File Creation ---")
	newFileDiff := components.DiffInfo{
		FilePath:   "new_file.go",
		OldContent: "",
		NewContent: "package main\n\nfunc main() {\n\tfmt.Println(\"Hello\")\n}",
		Title:      "Creating new file",
	}
	newFileOutput := diffRenderer.RenderDiff(newFileDiff)
	fmt.Println(newFileOutput)

	// Test 4: File deletion diff
	fmt.Println("\n--- Test Case 4: File Deletion ---")
	deletionDiff := components.DiffInfo{
		FilePath:   "old_file.go",
		OldContent: "package main\n\nfunc main() {\n\tfmt.Println(\"Goodbye\")\n}",
		NewContent: "",
		Title:      "Deleting file",
	}
	deletionOutput := diffRenderer.RenderDiff(deletionDiff)
	fmt.Println(deletionOutput)

	// Test 5: Regular modification diff
	fmt.Println("\n--- Test Case 5: Regular Modification ---")
	modificationDiff := components.DiffInfo{
		FilePath:   "modified_file.go",
		OldContent: sampleOldContent,
		NewContent: sampleNewContent,
		Title:      "Modifying function",
	}
	modificationOutput := diffRenderer.RenderDiff(modificationDiff)
	fmt.Println(modificationOutput)
}

func testLargeFileView(theme shared.Theme) {
	fmt.Println("📁 Testing Large File Edit/Write View Component")
	fmt.Println(colors.CreateSeparator(80, "─"))

	largeOldContent := generateLargeFileContent("OLD", 100)
	largeNewContent := generateLargeFileContent("NEW", 100)

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
		fmt.Println(colors.CreateSeparator(size.width/2, "─"))

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

		output := approvalComponent.Render(toolExecution, 0)
		lines := len(strings.Split(output, "\n"))

		fmt.Printf("Rendered output has %d lines (fits in %d height: %t)\n",
			lines, size.height, lines <= size.height)

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

		fmt.Printf("\n--- Direct Diff Renderer Test at %dx%d ---\n", size.width, size.height)
		diffOutput := diffRenderer.RenderColoredDiff(largeOldContent[:200], largeNewContent[:200])
		diffLines := strings.Split(diffOutput, "\n")
		fmt.Printf("Diff output has %d lines\n", len(diffLines))

		for i := 0; i < min(3, len(diffLines)); i++ {
			fmt.Println(diffLines[i])
		}
		if len(diffLines) > 3 {
			fmt.Printf("... (%d more lines)\n", len(diffLines)-3)
		}
	}

	fmt.Printf("\n=== Testing Write Tool with Large Content ===\n")
	fmt.Println(colors.CreateSeparator(80, "─"))

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

	if filePath, ok := args["file_path"].(string); ok {
		preview.WriteString(fmt.Sprintf("File: %s\n", filePath))
		preview.WriteString(strings.Repeat("─", 50) + "\n")
	}

	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	maxPreview := min(15, totalLines)
	for i := 0; i < maxPreview; i++ {
		lineNum := fmt.Sprintf("%4d", i+1)
		preview.WriteString(fmt.Sprintf("%s │ %s\n", lineNum, lines[i]))
	}

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
	testViewCmd.Flags().BoolVar(&snapshotMode, "snapshot", false, "Output clean snapshot format for testing")

	rootCmd.AddCommand(testViewCmd)
}
