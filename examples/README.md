# Inference Gateway CLI Examples

This directory contains practical examples for using the `infer` CLI tool to interact with the Inference Gateway.

## Prerequisites

1. Start the Inference Gateway server:
```bash
cp .env.example .env
docker run --rm -it --env-file .env -p 8080:8080 ghcr.io/inference-gateway/inference-gateway:latest
```

2. Install the CLI:
```bash
curl -fsSL https://raw.githubusercontent.com/inference-gateway/cli/main/install.sh | bash -s -- --install-dir $HOME/.local/bin
```

## Configuration

Set up your CLI configuration at `~/.infer.yaml`:

```yaml
gateway:
  url: "http://localhost:8080"
  api_key: ""
  timeout: 30
output:
  format: "text"  # text, json, yaml
  quiet: false
tools:
  enabled: false
```

## Basic Usage Examples

### Check Gateway Status
```bash
# Basic status check
infer status

# Detailed status with JSON output
infer status --detailed --format json
```

### List Deployed Models
```bash
infer models list
```

### Send Prompts
```bash
# Simple prompt
infer prompt "What is machine learning?"

# Interactive chat mode
infer chat
```

### Tool Management (Advanced)
```bash
# Enable tools in your config first, then:
infer tools list
infer tools exec "ls -la"
```

### Version Information
```bash
infer version
```

## Global Flags

All commands support these global flags:

- `--config`: Specify custom config file path
- `--verbose`: Enable verbose logging
- `--format`: Output format (text, json, yaml)
- `--quiet`: Suppress non-essential output

## Configuration Management

The CLI automatically creates a default configuration file at `~/.infer.yaml` on first run. You can customize:

- Gateway URL and API credentials
- Output formatting preferences
- Tool execution settings and security whitelist
- Request timeout values

For security, the tools feature requires explicit enablement and uses a whitelist approach for allowed commands.
