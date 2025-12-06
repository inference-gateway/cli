package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	mcp_golang "github.com/metoro-io/mcp-golang"
	"github.com/metoro-io/mcp-golang/transport/http"
)

// Tool argument structures with jsonschema tags

type GetTimeArgs struct {
	Timezone string `json:"timezone" jsonschema:"description=IANA timezone (e.g. America/New_York or UTC)"`
	Format   string `json:"format" jsonschema:"description=Time format: rfc3339 or unix"`
}

type CalculateArgs struct {
	Expression string `json:"expression" jsonschema:"required,description=Math expression like 2 + 2 or 10 * 5"`
}

type ListFilesArgs struct {
	Path    string `json:"path" jsonschema:"description=Directory path to list"`
	Pattern string `json:"pattern" jsonschema:"description=Optional glob pattern to filter files"`
}

type GetEnvArgs struct {
	Name string `json:"name" jsonschema:"required,description=Environment variable name"`
}

func main() {
	port := flag.Int("port", 3000, "Port to run the MCP server on")
	path := flag.String("path", "/mcp", "HTTP endpoint path")
	flag.Parse()

	// Create HTTP transport
	transport := http.NewHTTPTransport(*path)
	transport.WithAddr(fmt.Sprintf(":%d", *port))

	// Create MCP server
	server := mcp_golang.NewServer(transport)

	// Register tools
	registerTools(server)

	// Start server
	addr := fmt.Sprintf("http://localhost:%d", *port)
	log.Printf("🚀 Demo MCP Server starting on %s", addr)
	log.Printf("📝 MCP endpoint: %s%s", addr, *path)
	log.Printf("🔧 Available tools: get_time, calculate, list_files, get_env")
	log.Println()
	log.Printf("Configure in .infer/config.yaml:")
	log.Printf("  mcp:")
	log.Printf("    enabled: true")
	log.Printf("    servers:")
	log.Printf("      - name: demo-server")
	log.Printf("        url: %s%s", addr, *path)
	log.Printf("        enabled: true")
	log.Println()

	if err := server.Serve(); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func registerTools(server *mcp_golang.Server) {
	// Get Time tool
	if err := server.RegisterTool(
		"get_time",
		"Get the current system time in a specified timezone",
		handleGetTime,
	); err != nil {
		log.Fatalf("Failed to register get_time tool: %v", err)
	}

	// Calculate tool
	if err := server.RegisterTool(
		"calculate",
		"Perform basic arithmetic calculations",
		handleCalculate,
	); err != nil {
		log.Fatalf("Failed to register calculate tool: %v", err)
	}

	// List Files tool
	if err := server.RegisterTool(
		"list_files",
		"List files in a directory with optional pattern filtering",
		handleListFiles,
	); err != nil {
		log.Fatalf("Failed to register list_files tool: %v", err)
	}

	// Get Env tool
	if err := server.RegisterTool(
		"get_env",
		"Get an environment variable value",
		handleGetEnv,
	); err != nil {
		log.Fatalf("Failed to register get_env tool: %v", err)
	}
}

// Tool handlers

func handleGetTime(args GetTimeArgs) (*mcp_golang.ToolResponse, error) {
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
		return mcp_golang.NewToolResponse(
			mcp_golang.NewTextContent(fmt.Sprintf("Invalid timezone: %v", err)),
		), nil
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

	result := fmt.Sprintf("Current time in %s: %s", timezone, timeStr)
	return mcp_golang.NewToolResponse(mcp_golang.NewTextContent(result)), nil
}

func handleCalculate(args CalculateArgs) (*mcp_golang.ToolResponse, error) {
	expr := strings.TrimSpace(args.Expression)
	if expr == "" {
		return mcp_golang.NewToolResponse(
			mcp_golang.NewTextContent("Error: No expression provided"),
		), nil
	}

	result, err := evaluateExpression(expr)
	if err != nil {
		return mcp_golang.NewToolResponse(
			mcp_golang.NewTextContent(fmt.Sprintf("Calculation error: %v", err)),
		), nil
	}

	return mcp_golang.NewToolResponse(
		mcp_golang.NewTextContent(fmt.Sprintf("%s = %.2f", expr, result)),
	), nil
}

func handleListFiles(args ListFilesArgs) (*mcp_golang.ToolResponse, error) {
	path := args.Path
	if path == "" {
		path = "."
	}

	pattern := args.Pattern
	if pattern == "" {
		pattern = "*"
	}

	// Read directory
	entries, err := os.ReadDir(path)
	if err != nil {
		return mcp_golang.NewToolResponse(
			mcp_golang.NewTextContent(fmt.Sprintf("Failed to read directory: %v", err)),
		), nil
	}

	// Filter and format results
	var files []string
	for _, entry := range entries {
		name := entry.Name()

		// Apply pattern filter if specified
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
		return mcp_golang.NewToolResponse(
			mcp_golang.NewTextContent(fmt.Sprintf("No files found in %s matching pattern '%s'", path, pattern)),
		), nil
	}

	result := fmt.Sprintf("Files in '%s' (pattern: '%s'):\n%s\n\nTotal: %d items",
		path, pattern, strings.Join(files, "\n"), len(files))

	return mcp_golang.NewToolResponse(mcp_golang.NewTextContent(result)), nil
}

func handleGetEnv(args GetEnvArgs) (*mcp_golang.ToolResponse, error) {
	name := args.Name
	if name == "" {
		return mcp_golang.NewToolResponse(
			mcp_golang.NewTextContent("Error: No environment variable name provided"),
		), nil
	}

	value := os.Getenv(name)
	if value == "" {
		return mcp_golang.NewToolResponse(
			mcp_golang.NewTextContent(fmt.Sprintf("Environment variable '%s' is not set or empty", name)),
		), nil
	}

	return mcp_golang.NewToolResponse(
		mcp_golang.NewTextContent(fmt.Sprintf("%s=%s", name, value)),
	), nil
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
