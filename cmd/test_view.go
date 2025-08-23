package cmd

import (
	"fmt"
	"os"

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
  approval - Test the tool approval view with sample data
  diff     - Test the diff renderer with sample content`,
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
	default:
		fmt.Printf("Unknown component: %s\n", component)
		fmt.Println("Available components: approval, diff")
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
	default:
		return fmt.Sprintf("Mock formatting for %s with args: %v", toolName, args)
	}
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
