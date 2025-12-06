# MCP (Model Context Protocol) Integration

The Inference Gateway CLI supports direct integration with MCP (Model Context Protocol) servers, allowing you to extend
the LLM's capabilities with custom tools from external services.

## Table of Contents

- [Overview](#overview)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [Tool Discovery](#tool-discovery)
- [Liveness Probes](#liveness-probes)
- [Tool Execution](#tool-execution)
- [Examples](#examples)
- [Auto-Starting MCP Servers](#auto-starting-mcp-servers)
- [Troubleshooting](#troubleshooting)
- [Security Considerations](#security-considerations)
- [MCP vs A2A](#mcp-vs-a2a)
- [Advanced Topics](#advanced-topics)

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
- **Auto-start servers**: Automatically start and manage MCP servers in OCI/Docker containers
- **Automatic port assignment**: No need to manually configure ports for auto-started servers
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

#### Option A: Auto-start with OCI container (recommended)

Configure the server to start automatically when the CLI launches:

```yaml
servers:
  - name: "demo-server"
    enabled: true
    run: true  # Auto-start in container
    oci: "mcp-demo-server:latest"
    description: "Demo MCP server"
```

The CLI will automatically:

- Pull the OCI image if needed
- Start the container in the background
- Assign an available port (starting from 3000)
- Configure healthchecks
- Connect to the server

#### Option B: Manual start with Docker Compose

Run the included demo MCP server manually:

```bash
cd examples/mcp
docker compose up -d
```

Then configure the server URL:

```yaml
servers:
  - name: "demo-server"
    url: "http://localhost:3000/sse"
    enabled: true
```

The demo server provides four example tools: `get_time`, `calculate`, `list_files`, and `get_env`.

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
| `liveness_probe_enabled` | boolean | `false` | Enable health monitoring |
| `liveness_probe_interval` | integer | `10` | Health check interval (seconds) |
| `servers` | array | `[]` | List of MCP server configurations |

### Per-Server Configuration

Each server in the `servers` array supports:

| Field | Required | Type | Description |
| ----- | -------- | ---- | ----------- |
| `name` | ✅ | string | Unique server identifier |
| `url` | ❌* | string | HTTP SSE endpoint URL (*required if `run=false`) |
| `enabled` | ✅ | boolean | Enable/disable this server |
| `timeout` | ❌ | integer | Override global timeout |
| `description` | ❌ | string | Human-readable description |
| `include_tools` | ❌ | array | Whitelist specific tools |
| `exclude_tools` | ❌ | array | Blacklist specific tools |
| `run` | ❌ | boolean | Auto-start server in OCI container (default: false) |
| `host` | ❌ | string | Container host (default: localhost) |
| `scheme` | ❌ | string | URL scheme (default: http) |
| `port` | ❌ | integer | Simple port mapping (auto-assigned if omitted) |
| `ports` | ❌ | array | Advanced Docker-compose style port mappings |
| `path` | ❌ | string | HTTP path (default: /mcp) |
| `oci` | ❌* | string | OCI/Docker image (*required if `run=true`) |
| `args` | ❌ | array | Container startup arguments |
| `env` | ❌ | object | Environment variables for container |
| `volumes` | ❌ | array | Docker volume mounts |
| `startup_timeout` | ❌ | integer | Container startup timeout in seconds (default: 30) |
| `health_cmd` | ❌ | string | Custom Docker healthcheck command |

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

## Liveness Probes

The CLI includes health monitoring for MCP servers to detect disconnections and
display real-time connection status in the UI.

### Health Monitoring

1. **Background Monitoring**: Goroutines periodically ping each enabled MCP server
2. **Status Updates**: Connection changes trigger UI updates via event channels
3. **Real-time Display**: Status bar shows "MCP: X/Y" (connected/total)
4. **Auto-reconnection**: When server reconnects, tools become available immediately

### Probe Configuration

```yaml
liveness_probe_enabled: true   # Enable health monitoring
liveness_probe_interval: 10    # Seconds between health checks
```

### Status Display

The status bar displays current MCP server health:

```text
MCP: 0/1    # 0 connected, 1 total (server down)
MCP: 1/1    # 1 connected, 1 total (server healthy)
MCP: 2/3    # 2 connected, 3 total (1 server down)
```

### Probe Behavior

- **Initial State**: Shows total servers, all marked disconnected
- **First Connect**: When server responds to ping, status updates to connected
- **Disconnection**: Failed ping marks server as disconnected
- **Reconnection**: Successful ping after failure marks server as connected
- **Event-Driven**: UI updates only when status actually changes (no polling)

### Disabling Probes

To disable health monitoring:

```yaml
liveness_probe_enabled: false
```

With probes disabled, the MCP status will not appear in the status bar. Servers
are still checked during initial tool discovery at startup.

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

## Auto-Starting MCP Servers

### Auto-Start Overview

The CLI can automatically start and manage MCP servers as OCI/Docker containers. This provides:

- **Zero-configuration startup**: Servers start automatically when CLI launches
- **Automatic port assignment**: No need to manually configure ports
- **Container lifecycle management**: Start, stop, and health monitoring
- **Background execution**: Non-blocking startup, CLI ready immediately
- **Automatic healthchecks**: Docker healthchecks with MCP ping method

### Auto-Start Quick Start

Add a server with auto-start:

```bash
infer mcp add my-server \
  --description="My MCP server" \
  --run \
  --oci=my-mcp-server:latest
```

This automatically:

1. Creates server configuration with `run: true`
2. Assigns next available port (e.g., 3000, 3001, ...)
3. Configures container with defaults (localhost, http, /mcp path)
4. Adds Docker healthcheck using MCP ping method

### Auto-Start Configuration

Auto-start servers use component-based URL configuration instead of a single URL field:

```yaml
servers:
  - name: "my-server"
    enabled: true
    run: true                           # Enable auto-start
    oci: "my-mcp-server:latest"         # OCI/Docker image
    host: localhost                     # Default: localhost
    scheme: http                        # Default: http
    port: 3000                          # Auto-assigned if omitted
    path: /mcp                          # Default: /mcp
    startup_timeout: 60                 # Default: 30 seconds
```

The URL is constructed as: `{scheme}://{host}:{port}{path}`

### Port Assignment

**Automatic (recommended)**:

When adding servers without specifying `--port`, the CLI automatically:

1. Finds the highest port currently used by MCP servers
2. Assigns `basePort + 1` (starting from 3000)

```bash
infer mcp add server-1 --run --oci=image:latest  # Gets port 3000
infer mcp add server-2 --run --oci=image:latest  # Gets port 3001
infer mcp add server-3 --run --oci=image:latest  # Gets port 3002
```

**Manual**:

Specify a custom port:

```bash
infer mcp add my-server --run --oci=image:latest --port=8080
```

**Advanced port mappings**:

For complex scenarios, use `ports` array (Docker-compose style):

```yaml
servers:
  - name: "multi-port-server"
    run: true
    oci: "my-server:latest"
    ports:
      - "3000:8080"    # Host:Container
      - "3001:8081"
```

### Container Configuration

**Environment variables**:

```yaml
servers:
  - name: "api-server"
    run: true
    oci: "api-mcp:latest"
    env:
      API_KEY: "${MY_API_KEY}"          # Supports variable expansion
      LOG_LEVEL: "debug"
      DATABASE_URL: "postgres://..."
```

**Volume mounts**:

```yaml
servers:
  - name: "filesystem-server"
    run: true
    oci: "fs-mcp:latest"
    volumes:
      - "/host/path:/container/path"
      - "/data:/mnt/data:ro"            # Read-only
```

**Startup arguments**:

```yaml
servers:
  - name: "custom-server"
    run: true
    oci: "custom-mcp:latest"
    args:
      - "--verbose"
      - "--config=/etc/config.yaml"
```

**Custom healthcheck**:

```yaml
servers:
  - name: "api-server"
    run: true
    oci: "api-mcp:latest"
    health_cmd: 'sh -c "curl -f http://localhost:8080/health || exit 1"'
```

Default healthcheck (MCP ping):

```bash
sh -c 'curl -f -X POST http://localhost:3000/mcp \
  -H "Content-Type: application/json" \
  -d "{\"jsonrpc\":\"2.0\",\"method\":\"ping\",\"id\":1}" || exit 1'
```

### Lifecycle Management

**Container naming**: `inference-mcp-{server-name}`

**Network**: All containers join the `infer-network` Docker network

**Restart policy**: `unless-stopped` (containers restart on Docker daemon restart)

**Startup behavior**:

1. CLI checks if container already running (reuses if exists)
2. Pulls image if not cached locally
3. Starts container in background goroutine
4. Waits for healthcheck to pass (with timeout)
5. Logs success or failure (non-fatal)

**Shutdown**: Containers are stopped and removed when CLI exits

### CLI Commands

**Add server with auto-start**:

```bash
infer mcp add <name> [flags]
  --run                    # Enable auto-start
  --oci <image>            # OCI/Docker image (required if --run)
  --port <port>            # Optional: specific port
  --startup-timeout <sec>  # Optional: startup timeout (default: 60)
  --description <text>     # Optional: description
  --enabled                # Optional: enable immediately (default: true)
```

**Examples**:

```bash
# Minimal - automatic port assignment
infer mcp add demo --run --oci=mcp-demo:latest

# With custom port
infer mcp add api --run --oci=api-mcp:latest --port=8080

# With startup timeout
infer mcp add slow --run --oci=slow-mcp:latest --startup-timeout=120

# Complete configuration
infer mcp add custom \
  --run \
  --oci=custom-mcp:latest \
  --port=3000 \
  --startup-timeout=60 \
  --description="Custom MCP server" \
  --enabled
```

**Managing servers**:

```bash
# List servers
infer mcp list

# Remove server (stops container if running)
infer mcp remove <name>

# Toggle server
infer mcp toggle <name>
```

### Auto-Start Troubleshooting

**Container won't start**:

Check container logs:

```bash
docker logs inference-mcp-<name>
```

Check if port is already in use:

```bash
lsof -i :<port>
```

**Healthcheck failing**:

Verify the server's healthcheck endpoint:

```bash
curl -v http://localhost:<port>/health
```

Test MCP ping method:

```bash
curl -X POST http://localhost:<port>/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"ping","id":1}'
```

**Image not found**:

Build or pull the image manually:

```bash
docker pull <image>
# or
docker build -t <image> .
```

**Startup timeout**:

Increase timeout for slow-starting servers:

```yaml
servers:
  - name: "slow-server"
    startup_timeout: 120  # 2 minutes
```

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
