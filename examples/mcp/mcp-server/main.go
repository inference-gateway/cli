package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tool arguments. A jsonschema tag is the field's description; fields without
// omitempty are required.

type GetTimeArgs struct {
	Timezone string `json:"timezone,omitempty" jsonschema:"IANA timezone (e.g. America/New_York or UTC)"`
	Format   string `json:"format,omitempty" jsonschema:"Time format: rfc3339 or unix"`
}

type CalculateArgs struct {
	Expression string `json:"expression" jsonschema:"Math expression like 2 + 2 or 10 * 5"`
}

type ListFilesArgs struct {
	Path    string `json:"path,omitempty" jsonschema:"Directory path to list"`
	Pattern string `json:"pattern,omitempty" jsonschema:"Optional glob pattern to filter files"`
}

type GetEnvArgs struct {
	Name string `json:"name" jsonschema:"Environment variable name"`
}

func main() {
	port := flag.Int("port", 3000, "Port to run the MCP server on")
	path := flag.String("path", "/mcp", "HTTP endpoint path")
	flag.Parse()

	server := mcp.NewServer(&mcp.Implementation{Name: "demo-server", Version: "1.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "get_time", Description: "Get the current system time in a specified timezone"}, handleGetTime)
	mcp.AddTool(server, &mcp.Tool{Name: "calculate", Description: "Perform basic arithmetic calculations"}, handleCalculate)
	mcp.AddTool(server, &mcp.Tool{Name: "list_files", Description: "List files in a directory with optional pattern filtering"}, handleListFiles)
	mcp.AddTool(server, &mcp.Tool{Name: "get_env", Description: "Get an environment variable value"}, handleGetEnv)

	// 2026-07-28 requests are only served in stateless mode.
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true})

	mux := http.NewServeMux()
	mux.Handle(*path, handler)

	addr := fmt.Sprintf("http://localhost:%d", *port)
	log.Printf("🚀 Demo MCP Server (MCP 2026-07-28) starting on %s", addr)
	log.Printf("📝 MCP endpoint: %s%s", addr, *path)
	log.Printf("🔧 Available tools: get_time, calculate, list_files, get_env")

	if err := http.ListenAndServe(fmt.Sprintf(":%d", *port), mux); err != nil {
		log.Fatalf("Failed to start MCP server: %v", err)
	}
}

// text is a tool result carrying one text block.
func text(format string, args ...any) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}}}, nil, nil
}

// Tool handlers

func handleGetTime(_ context.Context, _ *mcp.CallToolRequest, args GetTimeArgs) (*mcp.CallToolResult, any, error) {
	timezone := args.Timezone
	if timezone == "" {
		timezone = "UTC"
	}

	format := args.Format
	if format == "" {
		format = "rfc3339"
	}

	location, err := time.LoadLocation(timezone)
	if err != nil {
		return text("Invalid timezone: %v", err)
	}

	now := time.Now().In(location)

	var timeStr string
	switch format {
	case "rfc3339":
		timeStr = now.Format(time.RFC3339)
	case "unix":
		timeStr = fmt.Sprintf("%d", now.Unix())
	default:
		timeStr = now.Format(format)
	}

	return text("Current time in %s: %s", timezone, timeStr)
}

func handleCalculate(_ context.Context, _ *mcp.CallToolRequest, args CalculateArgs) (*mcp.CallToolResult, any, error) {
	expr := strings.TrimSpace(args.Expression)
	if expr == "" {
		return text("Error: No expression provided")
	}

	result, err := evaluateExpression(expr)
	if err != nil {
		return text("Calculation error: %v", err)
	}

	return text("%s = %.2f", expr, result)
}

func handleListFiles(_ context.Context, _ *mcp.CallToolRequest, args ListFilesArgs) (*mcp.CallToolResult, any, error) {
	path := args.Path
	if path == "" {
		path = "."
	}

	pattern := args.Pattern
	if pattern == "" {
		pattern = "*"
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return text("Failed to read directory: %v", err)
	}

	var files []string
	for _, entry := range entries {
		name := entry.Name()

		if pattern != "*" {
			matched, err := filepath.Match(pattern, name)
			if err != nil || !matched {
				continue
			}
		}

		fileType := "file"
		if entry.IsDir() {
			fileType = "dir"
		}
		files = append(files, fmt.Sprintf("  %-40s [%s]", name, fileType))
	}

	if len(files) == 0 {
		return text("No files found in %s matching pattern '%s'", path, pattern)
	}

	return text("Files in '%s' (pattern: '%s'):\n%s\n\nTotal: %d items",
		path, pattern, strings.Join(files, "\n"), len(files))
}

func handleGetEnv(_ context.Context, _ *mcp.CallToolRequest, args GetEnvArgs) (*mcp.CallToolResult, any, error) {
	name := args.Name
	if name == "" {
		return text("Error: No environment variable name provided")
	}

	value := os.Getenv(name)
	if value == "" {
		return text("Environment variable '%s' is not set or empty", name)
	}

	return text("%s=%s", name, value)
}

// Helper functions

func evaluateExpression(expr string) (float64, error) {
	// Simple parser for basic operations
	var result float64
	var operator rune
	var currentNumber string

	for i, ch := range expr {
		if ch == ' ' {
			continue
		}

		if (ch >= '0' && ch <= '9') || ch == '.' {
			currentNumber += string(ch)
		} else if ch == '+' || ch == '-' || ch == '*' || ch == '/' {
			if currentNumber != "" {
				var num float64
				_, err := fmt.Sscanf(currentNumber, "%f", &num)
				if err != nil {
					return 0, fmt.Errorf("invalid number: %s", currentNumber)
				}

				if i == 0 || operator == 0 {
					result = num
				} else {
					result = applyOperation(result, num, operator)
				}
				currentNumber = ""
			}
			operator = ch
		} else {
			return 0, fmt.Errorf("invalid character: %c", ch)
		}
	}

	// Process last number
	if currentNumber != "" {
		var num float64
		_, err := fmt.Sscanf(currentNumber, "%f", &num)
		if err != nil {
			return 0, fmt.Errorf("invalid number: %s", currentNumber)
		}

		if operator == 0 {
			result = num
		} else {
			result = applyOperation(result, num, operator)
		}
	}

	return result, nil
}

func applyOperation(a, b float64, op rune) float64 {
	switch op {
	case '+':
		return a + b
	case '-':
		return a - b
	case '*':
		return a * b
	case '/':
		if b != 0 {
			return a / b
		}
		return 0
	}
	return 0
}
