# MCP Demo Server

A simple MCP (Model Context Protocol) server implementation in Go.

## Tools

- **get_time**: Get current time in any timezone
- **calculate**: Basic arithmetic calculations
- **list_files**: List files with pattern filtering
- **get_env**: Get environment variable values

## Running Standalone

```bash
cd mcp-server
go run main.go
```

Options:

```bash
go run main.go -port 8080 -path /mcp
```

The server will start on `http://localhost:3000/mcp` by default.
