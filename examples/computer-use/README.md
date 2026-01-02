# Computer Use Example

This example demonstrates the Inference Gateway CLI's computer use capabilities on a remote Ubuntu
desktop environment. The agent can control the GUI by taking screenshots, moving the mouse cursor,
clicking, and typing - all viewable through live screenshot streaming in your browser.

## Overview

This setup includes:

- **Ubuntu Desktop Server** with XFCE desktop environment
- **Xvfb (Headless X11 Server)** - lightweight headless display server
- **Screenshot Streaming** - live desktop view updated every 3 seconds
- **Inference Gateway** with computer use tools enabled
- **Web Interface** for interacting with the agent and viewing screenshots

The agent can control the remote Ubuntu GUI through specialized tools:

- `MouseMove`: Move the cursor to specific coordinates
- `MouseClick`: Click mouse buttons (left/right/middle, single/double/triple)
- `KeyboardType`: Type text or send key combinations (e.g., "ctrl+c")

**Note:** The Screenshot tool is automatically disabled when streaming is enabled - the LLM can see
live screenshots in the web UI overlay updated every 3 seconds.

## Architecture

```text
┌─────────────────┐          ┌──────────────────────┐          ┌──────────────────────┐
│  Your Browser   │◄─────────┤  Inference Gateway   │◄─────────┤  Ubuntu GUI Server   │
│  (Web UI)       │ WebSocket│  (Computer Use CLI)  │ SSH+HTTP │  - XFCE Desktop      │
│  + Screenshot   │          │  + Screenshot Proxy  │  Tunnel  │  - Xvfb Display :1   │
│    Overlay      │          └──────────────────────┘          │  - Screenshot API    │
└─────────────────┘                                            └──────────────────────┘
```

**Key Features:**

- **Live Screenshot Streaming**: View the desktop in real-time via browser overlay
- **Resource Efficient**: Xvfb uses ~10MB RAM vs VNC's ~100MB
- **HTTP Tunneling**: Screenshot streaming over SSH port forwarding
- **No VNC Client Needed**: Everything accessible via web browser

## Prerequisites

- Docker and Docker Compose
- At least 1GB RAM available
- Port 3000 (CLI web UI) available

## Quick Start

### 1. Start the Environment

```bash
docker-compose up -d --build
```

This will:

- Build the Ubuntu GUI server with XFCE desktop and Xvfb
- Start the Inference Gateway with computer use enabled
- Start screenshot streaming server on remote
- Set up SSH tunnel for screenshot API
- Expose the web interface on <http://localhost:3000>

### 2. Access the Web Interface

Open your browser to:

```text
http://localhost:3000
```

Click the "Screenshots" button in the top-right to toggle the live screenshot overlay.

### 3. View Live Screenshots

Once connected to the Ubuntu GUI Desktop server:

1. Click the **"Screenshots"** button in the top-right corner
2. The overlay will appear showing the remote desktop
3. Screenshots update every 3 seconds automatically
4. You can see exactly what the agent is doing in real-time

### 4. Try Computer Use Commands

In the web interface, try asking the agent to:

**Open an application:**

```text
Please click on the Applications menu at the top left (around coordinates 10, 10),
then navigate to and click on Firefox
```

**Type text:**

```text
Please type "Hello from the agent!" in the currently focused application
```

**Complex workflow:**

```text
Please open the Terminal Emulator application,
type "echo 'Agent was here' > /tmp/test.txt", and press Enter
```

**Note:** You don't need to ask the agent to take screenshots - they're automatically captured every 3 seconds and visible in the overlay!

## Configuration

The computer use tools are configured via environment variables in `docker-compose.yml`:

```yaml
environment:
  # X11 Display Configuration
  DISPLAY: :1
  SCREEN_RESOLUTION: 1280x720
  SCREEN_DEPTH: 24

  # Computer Use Tools
  INFER_COMPUTER_USE_ENABLED: "true"
  INFER_COMPUTER_USE_DISPLAY: ":1"

  # Screenshot Streaming (automatically disables Screenshot tool)
  INFER_COMPUTER_USE_SCREENSHOT_ENABLED: "true"
  INFER_COMPUTER_USE_SCREENSHOT_STREAMING_ENABLED: "true"
  INFER_COMPUTER_USE_SCREENSHOT_CAPTURE_INTERVAL: "3"  # seconds
  INFER_COMPUTER_USE_SCREENSHOT_BUFFER_SIZE: "30"      # keep last 30 screenshots

  # Mouse and Keyboard
  INFER_COMPUTER_USE_MOUSE_MOVE_ENABLED: "true"
  INFER_COMPUTER_USE_MOUSE_CLICK_ENABLED: "true"
  INFER_COMPUTER_USE_KEYBOARD_TYPE_ENABLED: "true"
```

You can also configure via `.infer/config.yaml`:

```yaml
computer_use:
  enabled: true
  display: ":1"

  screenshot:
    enabled: true
    streaming_enabled: true  # Disables Screenshot tool, enables streaming
    capture_interval: 3      # Capture every 3 seconds
    buffer_size: 30          # Keep last 30 screenshots

  mouse_move:
    enabled: true
    require_approval: true

  mouse_click:
    enabled: true
    require_approval: true

  keyboard_type:
    enabled: true
    require_approval: true

  rate_limit:
    enabled: true
    max_actions_per_minute: 60
```

## Security Features

### Approval System

By default, all mouse and keyboard actions require user approval:

- Screenshots are read-only and don't require approval
- Mouse movements/clicks require approval in the web UI
- Keyboard typing requires approval
- Use `--auto-accept` mode to bypass approvals (use with caution!)

### Rate Limiting

Prevents excessive actions:

- Default: 60 actions per minute
- Sliding window algorithm
- Configurable per deployment

### Auto-Accept Mode

For fully autonomous operation:

```bash
docker-compose exec gateway infer chat --web --agent-mode auto-accept
```

**⚠️ Warning:** Auto-accept mode allows the agent to control the GUI without approval. Only use in isolated/sandboxed environments.

## Available Tools

### Screenshot

```yaml
Name: Screenshot
Parameters:
  - region (optional): {x, y, width, height}
  - display (optional): default ":1"
```

### MouseMove

```yaml
Name: MouseMove
Parameters:
  - x: integer (required)
  - y: integer (required)
  - display (optional): default ":1"
```

### MouseClick

```yaml
Name: MouseClick
Parameters:
  - button: "left" | "right" | "middle" (required)
  - clicks: 1 | 2 | 3 (optional, default: 1)
  - x: integer (optional, move before clicking)
  - y: integer (optional, move before clicking)
  - display (optional): default ":1"
```

### KeyboardType

```yaml
Name: KeyboardType
Parameters:
  - text: string (optional, mutually exclusive with key_combo)
  - key_combo: string (optional, e.g., "ctrl+c", "alt+tab")
  - display (optional): default ":1"
```

## Example Workflows

### 1. Open and Use Firefox

```text
Agent: Please help me open Firefox and navigate to example.com

Steps:
1. Take a screenshot to see the current state
2. Click on Applications menu (top-left, ~10, 10)
3. Click on Web Browser
4. Wait for Firefox to open (take another screenshot)
5. Click on the address bar
6. Type "example.com"
7. Press Enter (send key combo "Return")
```

### 2. Create and Edit a Text File

```text
Agent: Create a text file with some content

Steps:
1. Take a screenshot
2. Click on Applications menu
3. Navigate to Accessories → Text Editor
4. Type "This is a test file created by the agent"
5. Press Ctrl+S to save
6. Type "/tmp/agent-test.txt" as filename
7. Press Enter to confirm
```

### 3. Run Terminal Commands

```text
Agent: Please run "ls -la" in the terminal

Steps:
1. Take a screenshot
2. Click on Applications menu
3. Click on Terminal Emulator
4. Type "ls -la"
5. Press Enter
6. Take a screenshot of the output
```

## Troubleshooting

### VNC Connection Refused

```bash
# Check if the GUI server is running
docker-compose ps ubuntu-gui

# View logs
docker-compose logs ubuntu-gui
```

### X11 Connection Issues

```bash
# Verify DISPLAY is set correctly
docker-compose exec ubuntu-gui echo $DISPLAY

# Should output: :1
```

### Computer Use Tools Not Available

```bash
# Check configuration
docker-compose exec gateway cat /workspace/.infer/config.yaml | grep -A 20 computer_use

# Ensure enabled: true
```

### Agent Can't Control GUI

```bash
# Verify X11 is running
docker-compose exec ubuntu-gui ps aux | grep X

# Check VNC server
docker-compose exec ubuntu-gui ps aux | grep vnc
```

## Cleanup

```bash
# Stop all services
docker-compose down

# Remove volumes (resets desktop state)
docker-compose down -v
```

## Advanced Usage

### Custom Desktop Environment

Edit `Dockerfile.ubuntu-gui` to use a different desktop environment:

```dockerfile
# Replace XFCE with MATE, LXDE, etc.
RUN apt-get install -y ubuntu-mate-desktop
```

### Custom Screen Resolution

Edit `docker-compose.yml`:

```yaml
environment:
  - VNC_RESOLUTION=1920x1080
```

### Persistent Desktop State

Mount a volume for the user home directory:

```yaml
volumes:
  - ubuntu-home:/home/ubuntu
```

## Implementation Details

### Display Server Detection

The CLI automatically detects X11 vs Wayland:

- Ubuntu GUI server uses X11 (DISPLAY=:1)
- Pure Go implementation using `xgb`/`xgbutil` libraries
- No CGO required for X11 support

### Screenshot Streaming

Screenshots are:

1. Captured via X11 protocol
2. Optimized (resized/compressed per config)
3. Base64 encoded
4. Sent via WebSocket to browser
5. Displayed in the web interface

### Mouse/Keyboard Control

Uses X11 protocol directly:

- `xproto.WarpPointer` for mouse movement
- `xproto.ButtonPress`/`ButtonRelease` for clicks (requires xdotool)
- `KeyPress`/`KeyRelease` for typing (requires keysym mapping)

**Note:** Full keyboard and mouse click support in pure Go is limited. The example uses command-line tools (`xdotool`, `xte`) as a fallback.

## Security Considerations

### Isolation

- Run in isolated Docker network
- Don't expose VNC port publicly
- Use strong VNC passwords in production
- Consider using SSH tunnels for VNC access

### Action Approval

- Always require approval for mouse/keyboard in production
- Use auto-accept mode only in sandboxed environments
- Monitor agent actions via web interface
- Enable rate limiting to prevent abuse

### Audit Logging

All computer use actions are logged:

- Timestamps
- Tool name (Screenshot, MouseMove, etc.)
- Arguments (coordinates, text, key combos)
- Success/failure status
- User approval decisions

Check logs:

```bash
docker-compose logs gateway | grep "computer_use"
```

## Further Reading

- [Anthropic Computer Use Guide](https://docs.anthropic.com/claude/docs/computer-use)
- [X11 Protocol Documentation](https://www.x.org/releases/current/doc/)
- [VNC Protocol](https://en.wikipedia.org/wiki/Virtual_Network_Computing)
- [XFCE Desktop Environment](https://www.xfce.org/)

## Contributing

Found an issue or want to improve this example? Please open an issue or PR in the main repository.
