# MCP (Model Context Protocol) Integration

The Inference Gateway CLI supports direct integration with MCP (Model Context Protocol) servers, allowing you to extend
the LLM's capabilities with custom tools from external services.

## Table of Contents

- [Overview](#overview)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [Tool Discovery](#tool-discovery)
- [Tool Execution](#tool-execution)
- [Examples](#examples)
- [Troubleshooting](#troubleshooting)
- [Security Considerations](#security-considerations)
- [MCP vs A2A](#mcp-vs-a2a)

## Overview

### What is MCP?

Model Context Protocol (MCP) is a standardized protocol for connecting AI models to external tools and data sources. It enables:

- **Stateless tool execution**: Each request is independent
- **HTTP SSE transport**: Server-Sent Events for real-time communication
- **Dynamic tool discovery**: Tools are discovered at runtime
- **Schema-based validation**: JSON Schema for tool parameters

### Architecture

```text
┌──────────────────────────────────────────────────────────────────┐
│                         Inference CLI                            │
│                                                                  │
│  ┌────────────────┐              ┌─────────────────┐             │
│  │ MCP Client     │              │  Tool Registry  │             │
│  │ Manager        │──register──▶ │                 │             │
│  │                │   tools      │  • Bash         │             │
│  │ • Discovery    │              │  • Read         │             │
│  │ • Execution    │              │  • MCP_*        │             │
│  └────────┬───────┘              └─────────────────┘             │
│           │                                                      │
└───────────┼──────────────────────────────────────────────────────┘
            │
            │ HTTP SSE (stateless)
            │
            ├────────────────────┬────────────────────┐
            │                    │                    │
            ▼                    ▼                    ▼
   ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐
   │  MCP Server     │  │  MCP Server     │  │  MCP Server     │
   │  (Filesystem)   │  │  (Database)     │  │  (Custom API)   │
   │                 │  │                 │  │                 │
   │  Tools:         │  │  Tools:         │  │  Tools:         │
   │  • read_file    │  │  • query        │  │  • fetch_data   │
   │  • write_file   │  │  • list_tables  │  │  • process      │
   │  • list_dir     │  │  • describe     │  │  • transform    │
   └─────────────────┘  └─────────────────┘  └─────────────────┘
```

### Key Features

- **Direct connections**: CLI connects directly to MCP servers (no gateway intermediary)
- **Stateless design**: Each tool execution creates a new HTTP connection
- **Per-server configuration**: Enable/disable servers independently
- **Tool filtering**: Include/exclude specific tools per server
- **Concurrent discovery**: Servers are queried in parallel
- **Resilient**: Failed servers don't prevent CLI startup
- **Mode-aware**: MCP tools automatically excluded from Plan mode

## Quick Start

### 1. Initialize Project

```bash
cd your-project
infer init
```

This creates `.infer/mcp.yaml` with example configuration.

### 2. Configure MCP Server

Edit `.infer/mcp.yaml`:

```yaml
enabled: true
connection_timeout: 30
discovery_timeout: 30

servers:
  - name: "filesystem"
    url: "http://localhost:3000/sse"
    enabled: true
    description: "File system operations"
```

### 3. Start MCP Server

Example using Node.js MCP server:

```bash
cd tests/mcp-test-server
npm install
npm start
```

### 4. Verify Tools

```bash
infer chat
```

Type `/help` to see available tools. MCP tools appear as `MCP_<server>_<tool>`.

## Configuration

### Global Settings

Located in `.infer/mcp.yaml`:

| Setting | Type | Default | Description |
| ------- | ---- | ------- | ----------- |
| `enabled` | boolean | `false` | Global MCP enable/disable toggle |
| `connection_timeout` | integer | `30` | Default connection timeout (seconds) |
| `discovery_timeout` | integer | `30` | Tool discovery timeout (seconds) |
| `servers` | array | `[]` | List of MCP server configurations |

### Per-Server Configuration

Each server in the `servers` array supports:

| Field | Required | Type | Description |
| ----- | -------- | ---- | ----------- |
| `name` | ✅ | string | Unique server identifier |
| `url` | ✅ | string | HTTP SSE endpoint URL |
| `enabled` | ✅ | boolean | Enable/disable this server |
| `timeout` | ❌ | integer | Override global timeout |
| `description` | ❌ | string | Human-readable description |
| `include_tools` | ❌ | array | Whitelist specific tools |
| `exclude_tools` | ❌ | array | Blacklist specific tools |

### Tool Filtering

#### Include Tools (Whitelist)

When `include_tools` is specified, **only** these tools are exposed:

```yaml
servers:
  - name: "database"
    url: "http://localhost:3001/sse"
    enabled: true
    include_tools:
      - "query"
      - "describe_table"
      - "list_tables"
```

#### Exclude Tools (Blacklist)

When `exclude_tools` is specified, these tools are hidden:

```yaml
servers:
  - name: "filesystem"
    url: "http://localhost:3000/sse"
    enabled: true
    exclude_tools:
      - "delete_file"  # Exclude dangerous operations
      - "format_disk"
```

#### Precedence

- If `include_tools` is set, it takes precedence (strict whitelist)
- If only `exclude_tools` is set, all tools except excluded ones are available
- If neither is set, all tools from the server are available

### Environment Variables

All configuration values support environment variable expansion:

```yaml
servers:
  - name: "filesystem"
    url: "${MCP_FILESYSTEM_URL}"
    enabled: true
```

Override via environment:

```bash
export INFER_MCP_ENABLED=true
export INFER_MCP_CONNECTION_TIMEOUT=60
export MCP_FILESYSTEM_URL=http://localhost:3000/sse
```

## Tool Discovery

### Discovery Process

1. **CLI startup**: MCP client manager initializes
2. **Concurrent discovery**: Each enabled server is queried in parallel
3. **Tool registration**: Discovered tools are registered in the tool registry
4. **Filtering applied**: Include/exclude rules are enforced
5. **Naming**: Tools are prefixed with `MCP_<server>_<tool>`

### Discovery Timeout

Configure discovery timeout to prevent slow servers from delaying startup:

```yaml
discovery_timeout: 30  # seconds
```

### Failed Servers

If a server fails during discovery:

- A warning is logged
- Other servers continue normally
- CLI starts successfully
- Failed server's tools are not available

Example log output:

```text
WARN Failed to discover tools from MCP server server=filesystem url=http://localhost:3000/sse error="connection refused"
INFO Discovered tools from MCP server server=database tool_count=5
```

## Tool Execution

### Execution Flow

1. **LLM requests tool**: `MCP_filesystem_read_file`
2. **Tool lookup**: Registry finds MCP tool wrapper
3. **Client creation**: New HTTP SSE client created (stateless)
4. **Server call**: Tool executed on MCP server
5. **Result formatting**: Response formatted for LLM/UI
6. **Connection closed**: HTTP connection terminated

### Timeouts

Per-server timeout configuration:

```yaml
servers:
  - name: "slow-server"
    url: "http://slow-service:8080/sse"
    timeout: 120  # Override global timeout
```

Timeout precedence:

1. Server-specific `timeout`
2. Global `connection_timeout`
3. Default: 30 seconds

### Error Handling

Common errors and handling:

| Error | Behavior |
| ----- | -------- |
| Server unreachable | Tool execution fails, error returned to LLM |
| Timeout exceeded | Connection closed, timeout error returned |
| Invalid arguments | Validation error before server call |
| Server error | Error response passed to LLM |

## Examples

### Example 1: Filesystem MCP Server

```yaml
# .infer/mcp.yaml
enabled: true
connection_timeout: 30
discovery_timeout: 30

servers:
  - name: "filesystem"
    url: "http://localhost:3000/sse"
    enabled: true
    description: "Sandboxed file system operations"
    exclude_tools:
      - "delete_file"
      - "delete_directory"
```

Available tools:

- `MCP_filesystem_read_file`
- `MCP_filesystem_write_file`
- `MCP_filesystem_list_directory`
- `MCP_filesystem_create_directory`
- `MCP_filesystem_get_file_info`

### Example 2: Multiple Servers

```yaml
enabled: true

servers:
  # Filesystem access
  - name: "filesystem"
    url: "http://localhost:3000/sse"
    enabled: true
    timeout: 60

  # Database queries
  - name: "postgres"
    url: "http://localhost:3001/sse"
    enabled: true
    include_tools:
      - "query"
      - "describe_table"

  # External API (disabled)
  - name: "weather-api"
    url: "http://localhost:3002/sse"
    enabled: false
```

### Example 3: Environment-Based Configuration

```yaml
# .infer/mcp.yaml
enabled: ${MCP_ENABLED:-false}
connection_timeout: ${MCP_TIMEOUT:-30}

servers:
  - name: "production-db"
    url: "${PROD_DB_MCP_URL}"
    enabled: ${PROD_DB_ENABLED:-false}
```

```bash
# .env
MCP_ENABLED=true
MCP_TIMEOUT=60
PROD_DB_MCP_URL=https://mcp.production.example.com/sse
PROD_DB_ENABLED=true
```

## Troubleshooting

### MCP Tools Not Appearing

**Check 1**: Is MCP enabled?

```yaml
enabled: true  # Must be true
```

**Check 2**: Are servers enabled?

```yaml
servers:
  - name: "my-server"
    enabled: true  # Must be true
```

**Check 3**: Check CLI logs

```bash
infer chat --log-level debug
```

Look for discovery messages:

```text
INFO Discovered tools from MCP server server=filesystem tool_count=8
```

### Connection Errors

**Error**: `connection refused`

**Causes**:

- MCP server not running
- Wrong URL/port
- Firewall blocking connection

**Solutions**:

- Verify server is running: `curl http://localhost:3000/sse`
- Check URL in config
- Check network connectivity

### Timeout Issues

**Error**: `context deadline exceeded`

**Causes**:

- Server response too slow
- Network latency
- Timeout too short

**Solutions**:

- Increase timeout:

  ```yaml
  servers:
    - name: "slow-server"
      timeout: 120  # Increase from default 30
  ```

- Check server performance
- Verify network connection

### Tool Filtering Not Working

**Issue**: Excluded tools still appear

**Check**: Verify exact tool names in logs:

```bash
infer chat --log-level debug 2>&1 | grep "Registered MCP tool"
```

**Fix**: Match exact tool names:

```yaml
exclude_tools:
  - "delete_file"  # Exact match required
```

### Plan Mode Still Shows MCP Tools

This is a bug - MCP tools should be automatically filtered in Plan mode. Please file an issue.

## Security Considerations

### 1. Tool Filtering

Always use `exclude_tools` to block dangerous operations:

```yaml
servers:
  - name: "filesystem"
    exclude_tools:
      - "delete_file"
      - "delete_directory"
      - "format_disk"
      - "execute_command"
```

### 2. Network Security

- Use HTTPS for production MCP servers
- Configure firewall rules
- Use VPN for remote servers
- Limit server access with authentication (if supported by MCP server)

### 3. Timeout Enforcement

Set reasonable timeouts to prevent hanging:

```yaml
connection_timeout: 30
discovery_timeout: 30
```

### 4. Input Validation

MCP tool wrappers validate arguments before sending to server. Invalid arguments are rejected before network calls.

### 5. Sandboxing

Run MCP servers in sandboxed environments:

- Docker containers
- Virtual machines
- Restricted user accounts

## MCP vs A2A

### When to Use MCP

- **Stateless operations**: Read data, query APIs, simple transformations
- **Fast execution**: Operations complete in seconds
- **Direct tool calls**: Single request/response
- **External services**: Databases, APIs, file systems

### When to Use A2A

- **Long-running tasks**: Operations taking minutes to hours
- **Complex workflows**: Multi-step processes
- **Background processing**: Async task execution
- **Agent delegation**: Specialized AI agents for specific domains

### Comparison

| Feature | MCP | A2A |
| ------- | --- | --- |
| **Connection** | Stateless HTTP SSE | Persistent |
| **Duration** | Seconds | Minutes to hours |
| **Use case** | Tool execution | Task delegation |
| **Polling** | N/A | Background monitoring |
| **Mode availability** | Standard, Auto-Accept | All modes |

## Advanced Topics

### Custom MCP Server

Create a custom MCP server using `@modelcontextprotocol/sdk` (Node.js):

```javascript
import { MCPServer } from '@modelcontextprotocol/sdk';
import { createServer } from 'http';

const mcp = new MCPServer({
  name: 'my-custom-server',
  version: '1.0.0'
});

// Register tools
mcp.tool('my_tool', {
  description: 'My custom tool',
  parameters: {
    type: 'object',
    properties: {
      input: { type: 'string' }
    }
  }
}, async (params) => {
  return { result: `Processed: ${params.input}` };
});

// Start HTTP SSE server
const server = createServer(mcp.createHTTPHandler());
server.listen(3000);
```

### Monitoring

Monitor MCP tool usage:

```bash
# Count MCP tool calls
cat .infer/logs/*.log | grep "MCP_" | wc -l

# Failed MCP calls
cat .infer/logs/*.log | grep "MCP.*failed"
```

## References

- [MCP Specification](https://github.com/anthropics/mcp)
- [MCP SDK (Node.js)](https://www.npmjs.com/package/@modelcontextprotocol/sdk)
- [MCP SDK (Go)](https://github.com/metoro-io/mcp-golang)
- [Example MCP Servers](../../tests/mcp-test-server/)

## Support

For issues or questions:

- [GitHub Issues](https://github.com/inference-gateway/cli/issues)
- [Discussions](https://github.com/inference-gateway/cli/discussions)
