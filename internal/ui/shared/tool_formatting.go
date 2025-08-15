package shared

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

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
func FormatToolCall(toolName string, args map[string]interface{}) string {
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
// This shows a compact "ToolName(args)" format with 2 lines of preview
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
					truncateText(searchResult.Results[0].Title, 60))
			} else {
				preview = "No results found"
			}
		}
	case "Bash":
		if bashResult, ok := result.Data.(*domain.BashToolResult); ok {
			if bashResult.ExitCode == 0 && bashResult.Output != "" {
				preview = truncateText(strings.TrimSpace(bashResult.Output), 60)
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
	case "Fetch":
		if fetchResult, ok := result.Data.(*domain.FetchResult); ok {
			domain := getDomainFromURL(fetchResult.URL)
			preview = fmt.Sprintf("Fetched %d bytes from %s", fetchResult.Size, domain)
		}
	default:
		if result.Success {
			preview = "Execution completed successfully"
		} else {
			preview = "Execution failed"
		}
	}

	return fmt.Sprintf("%s\n%s %s", toolCall, statusIcon, preview)
}

// FormatToolResultExpanded formats a tool execution result with full details
// This is shown when user presses Ctrl+R to expand
func FormatToolResultExpanded(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	var output strings.Builder

	output.WriteString(fmt.Sprintf("🔧 Tool: %s\n", result.ToolName))
	output.WriteString(fmt.Sprintf("⏱️  Duration: %s\n", result.Duration.String()))
	output.WriteString(fmt.Sprintf("📊 Status: %s\n", getStatusText(result.Success)))

	if result.Error != "" {
		output.WriteString(fmt.Sprintf("❌ Error: %s\n", result.Error))
	}

	if len(result.Arguments) > 0 {
		output.WriteString("\n📝 Arguments:\n")
		keys := make([]string, 0, len(result.Arguments))
		for key := range result.Arguments {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for _, key := range keys {
			output.WriteString(fmt.Sprintf("  %s: %v\n", key, result.Arguments[key]))
		}
	}

	if result.Data != nil {
		output.WriteString("\n📄 Result:\n")
		output.WriteString(formatToolSpecificData(result.ToolName, result.Data))
	}

	if len(result.Metadata) > 0 {
		output.WriteString("\n🏷️  Metadata:\n")

		keys := make([]string, 0, len(result.Metadata))
		for key := range result.Metadata {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for _, key := range keys {
			output.WriteString(fmt.Sprintf("  %s: %s\n", key, result.Metadata[key]))
		}
	}

	return output.String()
}

// getStatusText returns a human-readable status text
func getStatusText(success bool) string {
	if success {
		return "Success"
	}
	return "Failed"
}

// formatToolSpecificData formats the data section based on tool type
func formatToolSpecificData(toolName string, data interface{}) string {
	switch toolName {
	case "Bash":
		return formatBashToolData(data)
	case "Read":
		return formatReadToolData(data)
	case "Tree":
		return formatTreeToolData(data)
	case "Fetch":
		return formatFetchToolData(data)
	case "WebSearch":
		return formatWebSearchToolData(data)
	}

	if jsonData, err := json.MarshalIndent(data, "", "  "); err == nil {
		return string(jsonData)
	}

	return fmt.Sprintf("%+v", data)
}

func formatBashToolData(data interface{}) string {
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

func formatReadToolData(data interface{}) string {
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

func formatTreeToolData(data interface{}) string {
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

func formatFetchToolData(data interface{}) string {
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

func formatWebSearchToolData(data interface{}) string {
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
	output.WriteString("\nResults:\n")
	for i, result := range searchResult.Results {
		output.WriteString(fmt.Sprintf("%d. %s\n", i+1, result.Title))
		output.WriteString(fmt.Sprintf("   %s\n", result.URL))
		if result.Snippet != "" {
			output.WriteString(fmt.Sprintf("   %s\n", result.Snippet))
		}
		output.WriteString("\n")
	}
	return output.String()
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