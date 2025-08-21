package shared

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/inference-gateway/cli/internal/domain"
)

// FormatToolResultForUIResponsive formats tool execution results for UI display with responsive width
func FormatToolResultForUIResponsive(result *domain.ToolExecutionResult, terminalWidth int) string {
	content := FormatToolResultForUI(result)
	return FormatResponsiveMessage(content, terminalWidth)
}

// FormatToolResultExpandedResponsive formats a tool execution result with full details and responsive width
func FormatToolResultExpandedResponsive(result *domain.ToolExecutionResult, terminalWidth int) string {
	content := FormatToolResultExpanded(result)
	return FormatResponsiveMessage(content, terminalWidth)
}

// FormatToolCall formats a tool call for consistent display across the application
func FormatToolCall(toolName string, args map[string]any) string {
	if len(args) == 0 {
		return fmt.Sprintf("%s()", toolName)
	}

	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	argPairs := make([]string, 0, len(args))
	for _, key := range keys {
		argPairs = append(argPairs, fmt.Sprintf("%s=%v", key, args[key]))
	}

	return fmt.Sprintf("%s(%s)", toolName, joinArgs(argPairs))
}

// joinArgs joins argument pairs with commas, handling long argument lists
func joinArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}
	if len(args) == 1 {
		return args[0]
	}

	result := args[0]
	for i := 1; i < len(args); i++ {
		result += ", " + args[i]
	}
	return result
}

// FormatToolResultForUI formats tool execution results specifically for UI display
func FormatToolResultForUI(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	toolCall := FormatToolCall(result.ToolName, result.Arguments)

	var statusIcon string
	if result.Success {
		statusIcon = "✅"
	} else {
		statusIcon = "❌"
	}

	var preview string
	switch result.ToolName {
	case "WebSearch":
		if searchResult, ok := result.Data.(*domain.WebSearchResponse); ok {
			if len(searchResult.Results) > 0 {
				preview = fmt.Sprintf("Found %d results: %s", len(searchResult.Results),
					TruncateText(searchResult.Results[0].Title, 60))
			} else {
				preview = "No results found"
			}
		}
	case "Bash":
		if bashResult, ok := result.Data.(*domain.BashToolResult); ok {
			if bashResult.ExitCode == 0 && bashResult.Output != "" {
				preview = TruncateText(strings.TrimSpace(bashResult.Output), 60)
			} else if bashResult.ExitCode != 0 {
				preview = fmt.Sprintf("Exit code: %d", bashResult.ExitCode)
			} else {
				preview = "Command completed"
			}
		}
	case "Read":
		if readResult, ok := result.Data.(*domain.FileReadToolResult); ok {
			fileName := getFileName(readResult.FilePath)
			preview = fmt.Sprintf("Read %d bytes from %s", readResult.Size, fileName)
		}
	case "WebFetch":
		if fetchResult, ok := result.Data.(*domain.FetchResult); ok {
			domain := getDomainFromURL(fetchResult.URL)
			preview = fmt.Sprintf("Fetched %d bytes from %s", fetchResult.Size, domain)
		}
	case "TodoWrite":
		if _, ok := result.Data.(*domain.TodoWriteToolResult); ok {
			preview = formatTodoWriteToolData(result.Data)
		}
	default:
		if result.Success {
			preview = "Execution completed successfully"
		} else {
			preview = "Execution failed"
		}
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("%s\n", toolCall))
	output.WriteString(fmt.Sprintf("└─ %s %s", statusIcon, preview))

	return output.String()
}

// FormatToolResultExpanded formats a tool execution result with full details
// This is shown when user presses Ctrl+R to expand
func FormatToolResultExpanded(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	var output strings.Builder
	toolCall := FormatToolCall(result.ToolName, result.Arguments)

	output.WriteString(fmt.Sprintf("%s\n", toolCall))
	output.WriteString(fmt.Sprintf("├─ ⏱️  Duration: %s\n", result.Duration.String()))
	output.WriteString(fmt.Sprintf("├─ 📊 Status: %s\n", getStatusText(result.Success)))

	if result.Error != "" {
		output.WriteString(fmt.Sprintf("├─ ❌ Error: %s\n", result.Error))
	}

	if len(result.Arguments) > 0 {
		output.WriteString("├─ 📝 Arguments:\n")
		keys := make([]string, 0, len(result.Arguments))
		for key := range result.Arguments {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for i, key := range keys {
			if i == len(keys)-1 && result.Data == nil && len(result.Metadata) == 0 {
				output.WriteString(fmt.Sprintf("│  └─ %s: %v\n", key, result.Arguments[key]))
			} else {
				output.WriteString(fmt.Sprintf("│  ├─ %s: %v\n", key, result.Arguments[key]))
			}
		}
	}

	if result.Data != nil {
		formatResultData(&output, result)
	}

	if len(result.Metadata) > 0 {
		output.WriteString("└─ 🏷️  Metadata:\n")

		keys := make([]string, 0, len(result.Metadata))
		for key := range result.Metadata {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for i, key := range keys {
			if i == len(keys)-1 {
				output.WriteString(fmt.Sprintf("   └─ %s: %s\n", key, result.Metadata[key]))
			} else {
				output.WriteString(fmt.Sprintf("   ├─ %s: %s\n", key, result.Metadata[key]))
			}
		}
	}

	return output.String()
}

// formatResultData formats the result data section with proper indentation
func formatResultData(output *strings.Builder, result *domain.ToolExecutionResult) {
	hasMetadata := len(result.Metadata) > 0
	if hasMetadata {
		output.WriteString("├─ 📄 Result:\n")
	} else {
		output.WriteString("└─ 📄 Result:\n")
	}

	dataContent := formatToolSpecificData(result.ToolName, result.Data)
	lines := strings.Split(strings.TrimRight(dataContent, "\n"), "\n")
	for _, line := range lines {
		if hasMetadata {
			fmt.Fprintf(output, "│  %s\n", line)
		} else {
			fmt.Fprintf(output, "   %s\n", line)
		}
	}
}

// getStatusText returns a human-readable status text
func getStatusText(success bool) string {
	if success {
		return "Success"
	}
	return "Failed"
}

// formatToolSpecificData formats the data section based on tool type
func formatToolSpecificData(toolName string, data any) string {
	switch toolName {
	case "Bash":
		return formatBashToolData(data)
	case "Read":
		return formatReadToolData(data)
	case "Tree":
		return formatTreeToolData(data)
	case "WebFetch":
		return formatFetchToolData(data)
	case "WebSearch":
		return formatWebSearchToolData(data)
	case "TodoWrite":
		return formatTodoWriteToolData(data)
	}

	if jsonData, err := json.MarshalIndent(data, "", "  "); err == nil {
		return string(jsonData)
	}

	return fmt.Sprintf("%+v", data)
}

func formatBashToolData(data any) string {
	bashResult, ok := data.(*domain.BashToolResult)
	if !ok {
		return ""
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Command: %s\n", bashResult.Command))
	output.WriteString(fmt.Sprintf("Exit Code: %d\n", bashResult.ExitCode))
	if bashResult.Error != "" {
		output.WriteString(fmt.Sprintf("Error: %s\n", bashResult.Error))
	}
	if bashResult.Output != "" {
		output.WriteString(fmt.Sprintf("Output:\n%s\n", bashResult.Output))
	}
	return output.String()
}

func formatReadToolData(data any) string {
	readResult, ok := data.(*domain.FileReadToolResult)
	if !ok {
		return ""
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("File: %s\n", readResult.FilePath))
	if readResult.StartLine > 0 {
		output.WriteString(fmt.Sprintf("Lines: %d", readResult.StartLine))
		if readResult.EndLine > 0 && readResult.EndLine != readResult.StartLine {
			output.WriteString(fmt.Sprintf("-%d", readResult.EndLine))
		}
		output.WriteString("\n")
	}
	output.WriteString(fmt.Sprintf("Size: %d bytes\n", readResult.Size))
	if readResult.Error != "" {
		output.WriteString(fmt.Sprintf("Error: %s\n", readResult.Error))
	}
	if readResult.Content != "" {
		output.WriteString(fmt.Sprintf("Content:\n%s\n", readResult.Content))
	}
	return output.String()
}

func formatTreeToolData(data any) string {
	treeResult, ok := data.(*domain.TreeToolResult)
	if !ok {
		return ""
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Path: %s\n", treeResult.Path))
	output.WriteString(fmt.Sprintf("Max Depth: %d\n", treeResult.MaxDepth))
	output.WriteString(fmt.Sprintf("Max Files: %d\n", treeResult.MaxFiles))

	if treeResult.UsingNativeTree {
		output.WriteString("Using: Native tree command\n")
	} else {
		output.WriteString("Using: Built-in implementation\n")
	}

	if treeResult.Truncated {
		output.WriteString("⚠️  Output truncated due to limits\n")
	}

	if len(treeResult.ExcludePatterns) > 0 {
		output.WriteString(fmt.Sprintf("Excluded patterns: %d\n", len(treeResult.ExcludePatterns)))
	}

	output.WriteString("\nTree Structure:\n")
	output.WriteString(treeResult.Output)

	return output.String()
}

func formatFetchToolData(data any) string {
	fetchResult, ok := data.(*domain.FetchResult)
	if !ok {
		return ""
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("URL: %s\n", fetchResult.URL))
	if fetchResult.Status > 0 {
		output.WriteString(fmt.Sprintf("Status: %d\n", fetchResult.Status))
	}
	output.WriteString(fmt.Sprintf("Size: %d bytes\n", fetchResult.Size))
	if fetchResult.ContentType != "" {
		output.WriteString(fmt.Sprintf("Content-Type: %s\n", fetchResult.ContentType))
	}
	if fetchResult.Cached {
		output.WriteString("Source: Cache\n")
	} else {
		output.WriteString("Source: Live\n")
	}
	if len(fetchResult.Metadata) > 0 {
		output.WriteString("Metadata:\n")

		keys := make([]string, 0, len(fetchResult.Metadata))
		for key := range fetchResult.Metadata {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for _, key := range keys {
			output.WriteString(fmt.Sprintf("  %s: %s\n", key, fetchResult.Metadata[key]))
		}
	}
	if fetchResult.Content != "" {
		output.WriteString(fmt.Sprintf("Content:\n%s\n", fetchResult.Content))
	}
	return output.String()
}

func formatWebSearchToolData(data any) string {
	searchResult, ok := data.(*domain.WebSearchResponse)
	if !ok {
		return ""
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Query: %s\n", searchResult.Query))
	output.WriteString(fmt.Sprintf("Engine: %s\n", searchResult.Engine))
	output.WriteString(fmt.Sprintf("Results: %d/%d\n", len(searchResult.Results), searchResult.Total))
	output.WriteString(fmt.Sprintf("Search Time: %s\n", searchResult.Time.String()))
	if searchResult.Error != "" {
		output.WriteString(fmt.Sprintf("Error: %s\n", searchResult.Error))
	}

	if len(searchResult.Results) == 0 {
		output.WriteString("\nNo results found.")
		return output.String()
	}

	output.WriteString("\nResults:\n")
	for i, result := range searchResult.Results {
		title := result.Title
		if len(title) > 120 {
			title = TruncateText(title, 120)
		}
		output.WriteString(fmt.Sprintf("%d. %s\n", i+1, title))

		url := result.URL
		if len(url) > 80 {
			output.WriteString("   🔗 ")
			output.WriteString(WrapText(url, 76))
		} else {
			output.WriteString(fmt.Sprintf("   🔗 %s", url))
		}
		output.WriteString("\n")

		if result.Snippet != "" {
			snippet := result.Snippet
			if len(snippet) > 300 {
				snippet = TruncateText(snippet, 300)
			}
			wrappedSnippet := WrapText(snippet, 76)
			output.WriteString(fmt.Sprintf("   %s", wrappedSnippet))
			output.WriteString("\n")
		}
		output.WriteString("\n")
	}
	return output.String()
}

func formatTodoWriteToolData(data any) string {
	todoResult, ok := data.(*domain.TodoWriteToolResult)
	if !ok {
		return ""
	}

	var output strings.Builder

	output.WriteString(fmt.Sprintf("**Todo List** (%d/%d completed)\n\n", todoResult.CompletedTasks, todoResult.TotalTasks))

	if todoResult.TotalTasks > 0 {
		progressPercent := float64(todoResult.CompletedTasks) / float64(todoResult.TotalTasks)
		progressBar := createBubbleTeaProgressBar(progressPercent)
		output.WriteString(fmt.Sprintf("Progress: %s\n\n", progressBar))
	}

	for _, todo := range todoResult.Todos {
		var checkbox, content string

		switch todo.Status {
		case "completed":
			checkbox = "☑"
			content = CreateStrikethroughText(todo.Content)
		case "in_progress":
			checkbox = "☐"
			content = CreateColoredText(todo.Content, AccentColor)
		default:
			checkbox = "☐"
			content = todo.Content
		}

		output.WriteString(fmt.Sprintf("%s %s\n", checkbox, content))
	}

	return output.String()
}

// createBubbleTeaProgressBar creates a progress bar using Bubble Tea's progress component
func createBubbleTeaProgressBar(percent float64) string {
	prog := progress.New(progress.WithDefaultGradient())
	prog.Width = 25

	if percent >= 1.0 {
		prog = progress.New(progress.WithSolidFill("#22C55E"))
	} else if percent >= 0.5 {
		prog = progress.New(progress.WithSolidFill("#3B82F6"))
	} else {
		prog = progress.New(progress.WithDefaultGradient())
	}

	prog.Width = 25
	return prog.ViewAs(percent)
}

// Helper functions
func getFileName(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return path
}

func getDomainFromURL(url string) string {
	if strings.HasPrefix(url, "http://") {
		url = url[7:]
	} else if strings.HasPrefix(url, "https://") {
		url = url[8:]
	}

	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return url
}
