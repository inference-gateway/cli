# MCP Demo Server

A simple MCP (Model Context Protocol) server implementation in Go, built on the official
[Go SDK](https://github.com/modelcontextprotocol/go-sdk). Its stateless Streamable HTTP handler serves
MCP `2026-07-28`, the revision `infer` speaks.

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
