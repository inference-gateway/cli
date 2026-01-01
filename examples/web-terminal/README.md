# Web Terminal Docker Compose Example

This example demonstrates the Inference Gateway CLI web terminal with both **local** and **remote SSH** modes available simultaneously.

## Features

- **Browser-based terminal**: Access `infer chat` through your web browser
- **Local mode**: Run `infer chat` locally in the container
- **Remote SSH mode**: Connect to remote servers via SSH
- **Server dropdown**: Switch between local and remote servers in the UI
- **Auto-install**: Automatically installs `infer` on remote servers if missing
- **Zero configuration**: SSH keys generated automatically

## Quick Start

**Start everything:**

```bash
docker compose up -d
```

**Access the web UI:**

Open <http://localhost:3000>

You'll see a dropdown with three options:

- **Local** - Runs `infer chat` locally in the container
- **Remote Ubuntu Server** - Ubuntu 24.04 server with SSH (demonstrates auto-install)
- **Remote Alpine Server** - Alpine 3.23.0 server with SSH (demonstrates lightweight distribution)

## What's Running

When you run `docker compose up`, it starts:

1. **ssh-keygen** - Generates SSH keys in `.ssh-keys/` (if not already present)
2. **web-terminal** - Web UI server on port 3000
3. **inference-gateway** - Gateway for local mode
4. **remote-ubuntu** - Ubuntu 24.04 server with SSH (for demo)
5. **remote-alpine** - Alpine 3.23.0 server with SSH (for demo)

All services are connected on the `infer-network` Docker network.

## How It Works

### Local Mode

1. Select "Local" from the dropdown
2. Click "+ New Tab"
3. `infer chat` runs locally in the web-terminal container
4. Uses the inference-gateway service on port 8080

### Remote SSH Mode

1. Select "Remote Ubuntu Server" or "Remote Alpine Server" from the dropdown
2. Click "+ New Tab"
3. Web terminal connects to the selected remote server via SSH
4. Authenticates using automatically generated SSH keys
5. **Auto-installer runs** (first connection only):
   - Detects `infer` is not installed
   - Identifies OS and architecture (linux/arm64 or linux/amd64)
   - Downloads **portable binary** from GitHub releases (CGO-free, no SQLite dependencies)
   - Installs to `/home/developer/bin/infer`
   - Runs `infer init --userspace` to initialize configuration
   - Configures environment variables to connect to the shared gateway
   - Starts `infer chat` on the remote server
6. Terminal shows remote session output

**Note**: Release binaries are built without SQLite support (CGO_ENABLED=0) for maximum portability across different Linux distributions.
JSONL is the default storage backend and works out-of-the-box. PostgreSQL and Redis storage backends are also supported without requiring
additional system libraries.

## Configuration

The `config.yaml` file defines available servers:

```yaml
web:
  enabled: true
  ssh:
    enabled: true
    auto_install: true
    install_version: "latest"

  servers:
    - name: "Remote Ubuntu Server"
      id: "remote-ubuntu"
      remote_host: "remote-ubuntu"
      remote_user: "developer"
      remote_port: 22
      command_path: "/home/developer/bin/infer"
      install_path: "/home/developer/bin/infer"
      auto_install: true
      description: "Ubuntu 24.04 server (demonstrates auto-install via SSH)"
      tags:
        - remote
        - demo
        - ubuntu

    - name: "Remote Alpine Server"
      id: "remote-alpine"
      remote_host: "remote-alpine"
      remote_user: "developer"
      remote_port: 22
      command_path: "/home/developer/bin/infer"
      install_path: "/home/developer/bin/infer"
      auto_install: true
      description: "Alpine 3.23.0 server (demonstrates lightweight Linux distribution)"
      tags:
        - remote
        - demo
        - alpine
```

### Adding Your Own Remote Servers

To connect to your real servers, add them to `config.yaml`:

```yaml
servers:
  - name: "Production"
    id: "prod"
    remote_host: "prod.example.com"
    remote_user: "deployer"
    remote_port: 22
    description: "Production server"
```

**For production:**

- Use your own SSH keys (mount `~/.ssh` instead of `.ssh-keys`)
- Or use SSH agent: mount `$SSH_AUTH_SOCK:/ssh-agent`
- Add your servers to `config.yaml`

## SSH Keys

**For this demo:** SSH keys are auto-generated in `.ssh-keys/` directory.

**For production:** Use your own SSH keys:

```yaml
# docker-compose.yml
volumes:
  - ~/.ssh:/home/infer/.ssh:ro  # Use your SSH keys
```

Or use SSH agent for better security:

```yaml
environment:
  - SSH_AUTH_SOCK=/ssh-agent
volumes:
  - $SSH_AUTH_SOCK:/ssh-agent:ro
```

## Auto-Installation

The auto-installer runs when `infer` is not found on the remote server:

1. Checks if binary exists at `command_path`
2. If missing, detects OS and architecture (`uname -s && uname -m`)
3. Downloads from GitHub releases
4. Installs to `install_path` (default: `~/bin/infer`)
5. Verifies installation with `infer version`
6. Starts `infer chat`

Disable auto-install per server:

```yaml
servers:
  - name: "My Server"
    id: "my-server"
    remote_host: "server.example.com"
    remote_user: "user"
    auto_install: false  # Assumes infer is already installed
```

## Cleanup

```bash
docker compose down -v
rm -rf .ssh-keys  # Remove generated SSH keys
```

## Environment Variables

Configure via environment variables:

```bash
# Web server settings
export INFER_WEB_HOST=0.0.0.0
export INFER_WEB_PORT=3000

# SSH settings
export INFER_WEB_SSH_ENABLED=true
export INFER_WEB_SSH_AUTO_INSTALL=true
export INFER_WEB_SSH_INSTALL_VERSION=latest

# Start with env vars
docker compose up -d
```

## Troubleshooting

### SSH Connection Failed

Check logs:

```bash
docker compose logs web-terminal
docker compose logs remote-ubuntu  # or remote-alpine
```

Common issues:

- SSH keys not mounted correctly
- Remote server not accessible
- Firewall blocking SSH (port 22)

### Auto-Install Failing

Verify:

```bash
# Check remote server logs
docker compose logs remote-ubuntu  # or remote-alpine

# Test manual SSH connection
docker exec -it infer-web-terminal ssh developer@remote-ubuntu  # or developer@remote-alpine
```

Common issues:

- Network connectivity to GitHub
- Insufficient permissions on remote
- Unsupported OS/architecture

### Server Not in Dropdown

- Verify `config.yaml` is mounted correctly
- Check web-terminal logs for config errors
- Ensure `ssh.enabled: true` in config
- Restart containers after config changes

## Security Notes

**For this demo:**

- SSH keys are auto-generated (insecure for production)
- Remote server runs with NOPASSWD sudo (demo only)
- Keys stored in `.ssh-keys/` (add to `.gitignore`)

**For production:**

- Use SSH agent with your existing keys
- Restrict sudo access on remote servers
- Use SSH certificates for better key management
- Enable host key verification
- Rotate SSH keys regularly

## Next Steps

- Try all three modes in the dropdown (Local, Ubuntu, Alpine)
- Watch the auto-install process on first remote connection
- Compare behavior across different Linux distributions (Ubuntu vs Alpine)
- Add your own remote servers to `config.yaml`
- Explore multi-server tab management in the UI
