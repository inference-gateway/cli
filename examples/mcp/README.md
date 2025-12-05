# Demo MCP Server

A demonstration Model Context Protocol (MCP) server that showcases how to build and integrate MCP servers with the
Inference Gateway CLI.

## Overview

This demo server implements four simple but useful tools:

- **get_time**: Get the current system time in any timezone
- **calculate**: Perform basic arithmetic calculations
- **list_files**: List files in a directory with optional pattern filtering
- **get_env**: Retrieve environment variable values

## Quick Start

### 1. Install Dependencies

```bash
cd examples/mcp-server
go mod download
```

### 2. Start the Demo Server

```bash
go run main.go
```

The server will start on `http://localhost:3000` by default with the endpoint at `/mcp`.

**Options:**

```bash
# Change port
go run main.go -port 8080

# Change endpoint path
go run main.go -path /api/mcp

# Both
go run main.go -port 8080 -path /api/mcp
```

### 3. Configure the CLI

Create or update `.infer/config.yaml`:

```yaml
mcp:
  enabled: true
  connection_timeout: 30
  discovery_timeout: 30
  servers:
    - name: demo-server
      url: http://localhost:3000/mcp
      enabled: true
      description: "Demo MCP server with example tools"
```

### 4. Start the CLI

```bash
infer chat
```

The CLI will automatically discover the four tools from the demo server. You should see logs like:

```text
INFO Successfully registered MCP tools count=4
```

### 5. Try the Tools

Ask the LLM to use the tools:

#### Example 1: Get Time

```text
You: What time is it in Tokyo right now?
```

The LLM will use the `MCP_demo-server_get_time` tool with timezone "Asia/Tokyo".

#### Example 2: Calculate

```text
You: Calculate 123 + 456
```

The LLM will use the `MCP_demo-server_calculate` tool.

#### Example 3: List Files

```text
You: List all Go files in the current directory
```

The LLM will use the `MCP_demo-server_list_files` tool with pattern "*.go".

#### Example 4: Get Environment Variable

```text
You: What's my HOME directory?
```

The LLM will use the `MCP_demo-server_get_env` tool to get the HOME variable.

## Tool Details

### get_time

Get the current system time in any timezone.

**Arguments:**

- `timezone` (optional): IANA timezone (e.g., "America/New_York", "Asia/Tokyo", "UTC")
- `format` (optional): Time format - "rfc3339" or "unix"

**Example:**

```json
{
  "timezone": "Europe/London",
  "format": "rfc3339"
}
```

### calculate

Perform basic arithmetic calculations (addition, subtraction, multiplication, division).

**Arguments:**

- `expression` (required): Math expression like "2 + 2" or "10 * 5"

**Example:**

```json
{
  "expression": "15 * 3 + 10"
}
```

### list_files

List files in a directory with optional glob pattern filtering.

**Arguments:**

- `path` (optional): Directory path to list (defaults to ".")
- `pattern` (optional): Glob pattern like "*.go" or "test_*"

**Example:**

```json
{
  "path": "./examples",
  "pattern": "*.go"
}
```

### get_env

Get an environment variable value.

**Arguments:**

- `name` (required): Environment variable name

**Example:**

```json
{
  "name": "PATH"
}
```

## Building for Production

### Build Binary

```bash
go build -o mcp-demo-server main.go
```

### Run Binary

```bash
./mcp-demo-server -port 3000
```

### Docker

Create a `Dockerfile`:

```dockerfile
FROM golang:1.25.4-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
RUN go build -o mcp-demo-server main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /root/
COPY --from=builder /app/mcp-demo-server .
EXPOSE 3000
CMD ["./mcp-demo-server"]
```

Build and run:

```bash
docker build -t mcp-demo-server .
docker run -p 3000:3000 mcp-demo-server
```

## Troubleshooting

### Server Not Starting

Check if the port is already in use:

```bash
lsof -i :3000
```

Use a different port:

```bash
go run main.go -port 8080
```

### Tools Not Discovered

1. Check server is running: `curl http://localhost:3000/mcp`
2. Check CLI config has correct URL
3. Enable debug logging: `infer chat --log-level debug`
4. Look for discovery errors in logs

### Connection Refused

Make sure the server URL in config matches where the server is running:

```yaml
# Config
url: http://localhost:3000/mcp

# Must match server startup
go run main.go -port 3000 -path /mcp
```

## Extending the Demo

To add more tools, follow this pattern:

1. **Define argument struct** with jsonschema tags:

```go
type MyToolArgs struct {
    Param1 string `json:"param1" jsonschema:"required,description=My parameter"`
}
```

2. **Implement handler function**:

```go
func handleMyTool(args MyToolArgs) (*mcp_golang.ToolResponse, error) {
    result := fmt.Sprintf("Processed: %s", args.Param1)
    return mcp_golang.NewToolResponse(mcp_golang.NewTextContent(result)), nil
}
```

3. **Register the tool**:

```go
server.RegisterTool(
    "my_tool",
    "Description of what my tool does",
    handleMyTool,
)
```

## Resources

- [MCP Golang Library](https://github.com/metoro-io/mcp-golang)
- [MCP Specification](https://modelcontextprotocol.io)
- [Inference Gateway CLI Documentation](../../docs/mcp-integration.md)
