# Web Terminal Interface

The Inference Gateway CLI includes a browser-based terminal interface that provides remote access to the chat
interface through any web browser. This enables multiple parallel sessions, remote access, and team collaboration
workflows.

## Table of Contents

- [Overview](#overview)
- [Quick Start](#quick-start)
- [Architecture](#architecture)
- [Configuration](#configuration)
- [Session Management](#session-management)
- [Use Cases](#use-cases)
- [Security Considerations](#security-considerations)
- [Troubleshooting](#troubleshooting)

## Overview

The web terminal feature wraps the standard `infer chat` TUI in a PTY (pseudo-terminal) and exposes it through a
WebSocket-based web interface using xterm.js. Each browser tab creates an independent terminal session with its own
`infer chat` process, containers, and resource isolation.

**Key Features:**

- **Browser-based access**: Access the CLI from any device with a web browser
- **Multiple sessions**: Create unlimited independent chat sessions in separate browser tabs
- **Automatic cleanup**: Sessions automatically terminate after configurable inactivity period
- **Container isolation**: Each tab manages its own Docker containers (agents, MCP servers)
- **Graceful shutdown**: Proper cleanup of all sessions and containers on server shutdown
- **Responsive UI**: Terminal automatically resizes to fit browser window

## Quick Start

### Start the Web Terminal Server

```bash
# Start with default settings (localhost:3000)
infer chat --web

# Custom port
infer chat --web --port 8080

# Bind to all interfaces for remote access (SECURITY WARNING: see below)
infer chat --web --host 0.0.0.0 --port 8080
```

The server will output:

```text
🌐 Web terminal available at: http://localhost:3000
   Open this URL in your browser to access the terminal.
```

### Using the Web Interface

1. **Open the URL** in your browser (e.g., <http://localhost:3000>)
2. **Create sessions** by clicking the "+" button in the tab bar
3. **Switch sessions** by clicking on tab titles
4. **Close sessions** by clicking the "×" button on tabs
5. **Shutdown server** with Ctrl+C in the terminal

Each tab provides a full `infer chat` session with:

- Model selection
- Real-time streaming responses
- Tool execution with approval modals
- Agent mode switching (Standard/Plan/Auto-Accept)
- All shortcuts and commands available

## Architecture

### Components

```text
┌──────────────────┐       WebSocket       ┌──────────────────┐       PTY I/O        ┌─────────────────┐
│  Browser         │ ◄───────────────────► │  HTTP Server     │ ◄──────────────────► │ infer chat      │
│  (xterm.js)      │  Bidirectional        │  + Session Mgr   │  stdin/stdout/resize │ (BubbleTea TUI) │
└──────────────────┘                       └──────────────────┘                       └─────────────────┘
```

**HTTP Server** (`internal/web/server.go`):

- Serves HTML template and static assets (xterm.js, CSS, JavaScript)
- Handles WebSocket upgrade requests
- Manages graceful shutdown with signal handling

**Session Manager** (`internal/web/session_manager.go`):

- Tracks all active sessions with last activity timestamps
- Spawns cleanup goroutine for inactive session removal
- Coordinates shutdown of all sessions
- Thread-safe with mutex-protected session map

**PTY Manager** (`internal/web/pty_manager.go`):

- Spawns `infer chat` subprocess in PTY
- Bridges WebSocket ↔ PTY bidirectional I/O
- Handles terminal resize events
- Graceful process shutdown (PTY close → SIGTERM → SIGKILL)

**Frontend** (`internal/web/static/app.js`, `internal/web/templates/index.html`):

- Tab management for multiple sessions
- xterm.js terminal emulator with Tokyo Night theme
- Automatic terminal resizing with FitAddon
- WebSocket connection management

### Session Lifecycle

1. **Connection**: Browser opens WebSocket to `/ws`
2. **Session Creation**: Server creates new session ID and spawns PTY with `infer chat`
3. **Activity Tracking**: SessionHandler updates last activity timestamp every 10 seconds
4. **I/O Bridging**: Bidirectional data flow between WebSocket and PTY
5. **Cleanup**: On connection close or inactivity timeout, PTY process is shut down gracefully

### Container Management

Each session spawns its own `infer chat` process, which may create Docker containers for:

- A2A agents (e.g., browser-agent, code-reviewer)
- MCP servers with `run: true` configuration

When a session closes:

1. ServiceContainer.Shutdown() is called via signal handling
2. All containers are stopped with 20-second timeout
3. Ports are released from global port registry
4. Resources are freed

## Configuration

### Command-Line Flags

```bash
infer chat --web [--port PORT] [--host HOST]
```

**Flags:**

- `--web`: Enable web terminal mode
- `--port`: Port number (default: 3000)
- `--host`: Host to bind to (default: localhost)

### Configuration File

Add to `.infer/config.yaml`:

```yaml
web:
  enabled: true                # Enable web terminal
  port: 3000                   # Server port
  host: "localhost"            # Host binding
  session_inactivity_mins: 5   # Session timeout in minutes
```

### Environment Variables

```bash
export INFER_WEB_PORT=3000
export INFER_WEB_HOST="localhost"
export INFER_WEB_SESSION_INACTIVITY_MINS=10
```

### Configuration Precedence

1. Command-line flags (highest priority)
2. Environment variables
3. Config file
4. Built-in defaults

Example:

```bash
# Config file sets port to 3000
# Command-line overrides to 8080
infer chat --web --port 8080  # Uses 8080
```

## Session Management

### Automatic Cleanup

Sessions are automatically cleaned up after the configured inactivity period (default: 5 minutes).

**Activity tracking:**

- SessionHandler updates last activity every 10 seconds while connection is open
- Cleanup goroutine runs every 1 minute
- Sessions inactive beyond threshold are stopped and removed

**Logs:**

```text
INFO Cleaning up inactive session id=abc-123 inactive_duration=5m2s threshold=5m0s
INFO Inactive sessions cleaned up count=2 remaining=3 threshold=5m0s
```

### Manual Session Cleanup

Close a tab by clicking the "×" button. This:

1. Closes the WebSocket connection
2. Triggers graceful shutdown of the PTY process
3. Stops all associated containers
4. Removes session from tracking

### View Active Sessions

The server logs active session count:

```text
INFO Session created id=session-1 total=1
INFO Session created id=session-2 total=2
INFO Session removed id=session-1 total=1
```

### Shutdown All Sessions

Press Ctrl+C in the terminal running the web server:

```text
Shutting down web terminal server...
Stopping 3 active session(s)...
All sessions stopped
Stopping HTTP server...
Web terminal server stopped gracefully
```

## Use Cases

### 1. Remote Access

Access the CLI from any device on your network:

```bash
# Start server on your workstation
infer chat --web --host 0.0.0.0 --port 8080

# Access from laptop, tablet, or phone
# Navigate to: http://workstation-ip:8080
```

⚠️ **Security Warning**: Binding to `0.0.0.0` exposes the server to your entire network. Only do this on trusted networks.

### 2. Multiple Parallel Sessions

Work on multiple tasks simultaneously without terminal multiplexer:

- Tab 1: Chat with `claude-4.1-haiku` for quick questions
- Tab 2: Long-running agent task with `deepseek-chat`
- Tab 3: Testing with `gpt-4o`

Each tab is completely independent with its own model, conversation history, and containers.

### 3. Team Collaboration

Share terminal access for pair programming or troubleshooting:

```bash
# Start server accessible on LAN
infer chat --web --host 0.0.0.0

# Team members connect to http://your-ip:3000
# Each person gets their own tab/session
```

### 4. Long-Running Sessions

Keep sessions alive in background:

- Start web server
- Open browser, create session
- Minimize browser (session keeps running)
- Return hours later, tab still active
- Or wait for auto-cleanup after inactivity timeout

### 5. Mobile Access

Access from mobile devices where installing the CLI is impractical:

- iPhone/iPad: Safari or Chrome
- Android: Chrome or Firefox
- Terminal emulation with touch keyboard support

## Security Considerations

### Default Security Posture

By default, the web terminal is **secure for local development**:

- Binds to `localhost` only (not accessible from network)
- No authentication required (assumes trusted local user)
- Single-user assumed

### Remote Access Security

When binding to `0.0.0.0` or non-localhost addresses:

⚠️ **CRITICAL WARNINGS:**

1. **No Authentication**: Anyone who can reach the server can execute commands
2. **Full CLI Access**: Users have same permissions as the CLI process
3. **Tool Execution**: Can run bash commands, modify files, spawn containers
4. **API Key Access**: Can use configured API keys for LLM providers
5. **Network Exposure**: Port scanning may discover the service

**Recommendations for remote access:**

1. **Use SSH tunnel** instead of binding to 0.0.0.0:

   ```bash
   # On server
   infer chat --web

   # On client
   ssh -L 3000:localhost:3000 user@server
   # Access at http://localhost:3000
   ```

2. **Restrict host binding** to specific interface:

   ```bash
   # Bind to private network interface only
   infer chat --web --host 192.168.1.100
   ```

3. **Firewall rules**: Block port from untrusted networks

4. **VPN**: Only expose on VPN interface

5. **Reverse proxy with auth**: Use nginx/Caddy with authentication

Example nginx config with basic auth:

```nginx
server {
    listen 80;
    server_name terminal.example.com;

    auth_basic "CLI Access";
    auth_basic_user_file /etc/nginx/.htpasswd;

    location / {
        proxy_pass http://localhost:3000;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
```

### Future Authentication

Authentication and authorization features are planned for future releases. For now, use network-level controls for security.

## Troubleshooting

### Port Already in Use

**Error:**

```text
Error: listen tcp :3000: bind: address already in use
```

**Solutions:**

```bash
# Use different port
infer chat --web --port 8080

# Or kill process using port
lsof -ti:3000 | xargs kill -9

# Or find and choose available port
netstat -an | grep LISTEN | grep 3000
```

### WebSocket Connection Failed

**Symptom:** Browser console shows "WebSocket error" or "Connection closed"

**Solutions:**

1. **Check server is running:**

   ```bash
   curl http://localhost:3000
   # Should return HTML
   ```

2. **Check browser console** for detailed error
3. **Verify WebSocket endpoint:**

   ```javascript
   // Should connect to ws://localhost:3000/ws
   ```

4. **Check for proxy/firewall** blocking WebSocket connections

### Containers Not Cleaning Up

**Symptom:** Docker containers remain after closing tabs

**Debugging:**

```bash
# List all containers
docker ps -a

# Check if containers have CLI labels
docker ps -a --filter "label=created_by=infer"
```

**Solution:** Ensure signal handling is working:

- ServiceContainer.Shutdown() must complete
- Check logs for "Shutting down session manager"
- Increase shutdown timeout in config if needed

### Session Hangs on Close

**Symptom:** Tab close button doesn't respond, or server hangs on shutdown

**Debugging:**

```bash
# Check logs for shutdown sequence
# Should see:
# - "Stopping PTY session..."
# - "Initiating graceful shutdown of PTY process"
# - "PTY process exited gracefully after PTY close"
```

**Solutions:**

1. **Force kill stuck process:**

   ```bash
   # Find PID from logs
   kill -9 <pid>
   ```

2. **Restart server** if cleanup is stuck

3. **Check for deadlock** in logs (should not happen with current implementation)

### High Memory Usage

**Symptom:** Server memory grows with many sessions

**Explanation:** Each session spawns:

- PTY process (BubbleTea TUI)
- Potential Docker containers (agents/MCP)
- WebSocket connection buffers

**Solutions:**

1. **Lower inactivity timeout:**

   ```yaml
   web:
     session_inactivity_mins: 2  # Aggressive cleanup
   ```

2. **Limit concurrent sessions:** Close unused tabs

3. **Monitor with:**

   ```bash
   # Session count in logs
   grep "total" server.log

   # Container count
   docker ps | wc -l

   # Memory usage
   docker stats
   ```

### Static Files Not Loading

**Symptom:** Browser shows blank page or "404 Not Found" for CSS/JS

**Debugging:**

```bash
# Check embedded files
grep "Embedded file found" server.log

# Should show:
# static/xterm.js
# static/xterm.css
# static/xterm-addon-fit.js
# static/app.js
```

**Solution:** Rebuild binary to re-embed static files:

```bash
task build
# Or
go build -o infer .
```

### Terminal Size Issues

**Symptom:** Text cut off or excessive padding

**Solutions:**

1. **Browser zoom:** Reset to 100%
2. **Window resize:** Terminal auto-resizes with window
3. **Manual resize:** Trigger browser window resize event
4. **Check CSS:** Padding configured in `index.html` template

Current padding:

```css
.terminal-tab {
    top: 5px;           /* Top margin */
    padding: 0 25px;    /* Horizontal padding only */
}
```

### Cannot Access from Mobile

**Symptom:** Connection refused from phone/tablet

**Solutions:**

1. **Check host binding:**

   ```bash
   # Must bind to 0.0.0.0 or LAN IP
   infer chat --web --host 0.0.0.0
   ```

2. **Check firewall:** Allow incoming on port 3000

3. **Verify network:** Devices on same network/VLAN

4. **Use IP address:** <http://192.168.1.100:3000> instead of hostname

5. **Check router:** Some routers block client-to-client traffic

## Advanced Configuration

### Custom Session Timeout

For long-running tasks:

```yaml
web:
  session_inactivity_mins: 60  # 1 hour timeout
```

For aggressive cleanup:

```yaml
web:
  session_inactivity_mins: 1   # 1 minute timeout
```

Disable timeout (not recommended):

```yaml
web:
  session_inactivity_mins: 0   # Never cleanup (requires manual close)
```

### Multiple Servers

Run multiple servers on different ports:

```bash
# Terminal 1: Personal use
infer chat --web --port 3000

# Terminal 2: Team access
infer chat --web --port 8080 --host 0.0.0.0
```

Each server maintains independent sessions.

### Docker Deployment

Run the web terminal in a Docker container:

```dockerfile
FROM ghcr.io/inference-gateway/cli:latest

EXPOSE 3000

CMD ["chat", "--web", "--host", "0.0.0.0"]
```

Build and run:

```bash
docker build -t infer-web .
docker run -d -p 3000:3000 --env-file .env infer-web
```

**Note:** Requires Docker-in-Docker or Docker socket mount for container management:

```bash
docker run -d -p 3000:3000 \
  -v /var/run/docker.sock:/var/run/docker.sock \
  --env-file .env \
  infer-web
```

## Technical Details

### Libraries Used

- **xterm.js**: Browser terminal emulator (v5.3.0)
- **xterm-addon-fit**: Auto-sizing terminal to container
- **gorilla/websocket**: WebSocket server implementation
- **creack/pty**: PTY management for subprocess spawning

### File Structure

```text
cli/
├── cmd/
│   └── chat.go                        # --web flag handling
├── config/
│   ├── config.go                      # WebConfig struct
│   └── defaults.go                    # Default web config
└── internal/
    └── web/
        ├── server.go                  # HTTP + WebSocket server
        ├── session_manager.go         # Session lifecycle
        ├── pty_manager.go             # PTY subprocess control
        ├── static/                    # Embedded assets
        │   ├── xterm.js               # Terminal emulator
        │   ├── xterm.css              # Terminal styles
        │   ├── xterm-addon-fit.js     # Fit addon
        │   └── app.js                 # Tab management
        └── templates/
            └── index.html             # HTML template
```

### Port Allocation

Global port registry prevents race conditions:

```go
// internal/config/config.go
var (
    allocatedPorts = make(map[int]bool)
    portMutex      sync.Mutex
)

func FindAvailablePort(basePort int) int {
    portMutex.Lock()
    defer portMutex.Unlock()

    // Check port availability and register atomically
    // ...
}
```

Used by Docker containers spawned by agents/MCP servers.

### Graceful Shutdown

Signal handling ensures cleanup:

```go
// cmd/chat.go
sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)

defer func() {
    ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
    defer cancel()
    services.Shutdown(ctx)
}()
```

Shutdown sequence:

1. Ctrl+C sends SIGINT
2. Signal handler calls ServiceContainer.Shutdown()
3. Session manager stops all sessions
4. Each session shuts down PTY process gracefully
5. ServiceContainer stops all containers
6. HTTP server shuts down with 5-second timeout

## Future Enhancements

Planned features for future releases:

- **Authentication**: Basic auth, OAuth, API keys
- **Authorization**: Per-user session limits, tool restrictions
- **SSH Sessions**: Connect to remote servers via SSH
- **Session persistence**: Save/restore conversations across restarts
- **Shared sessions**: Multiple users in same terminal (read-only viewers)
- **Recording**: Save terminal output for later review
- **Themes**: Configurable color schemes
- **Keybindings**: Browser-compatible keyboard shortcuts
- **File uploads**: Drag-and-drop files into terminal
- **Mobile improvements**: Better touch keyboard support

## References

- [xterm.js Documentation](https://xtermjs.org/)
- [gorilla/websocket](https://github.com/gorilla/websocket)
- [creack/pty](https://github.com/creack/pty)
- [Model Context Protocol (MCP)](docs/mcp-integration.md)
- [Agent Configuration](docs/agents-configuration.md)
- [Configuration Reference](docs/configuration-reference.md)
