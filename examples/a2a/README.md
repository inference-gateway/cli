# Inference Gateway CLI Examples

This directory contains practical examples for using the `infer` CLI tool to interact with the Inference Gateway.

## Prerequisites

1. Configure the Inference Gateway server:

```bash
# Configure the providers you want to work with and some of the agents credentials (Google Calendar, Context7 - if applicable)
cp .env.example .env
```

2. Bring all the containers up:

```bash
docker compose up -d
```

3. Log the containers verify that everything is up and running:

```bash
docker compose ps
docker compose logs -f
```

## Configuration

Set up your CLI configuration via environment variables (review docker-compose.yaml cli service):

```yaml
INFER_GATEWAY_URL: http://inference-gateway:8080
INFER_A2A_ENABLED: true
INFER_TOOLS_ENABLED: false
INFER_AGENT_MODEL: deepseek/deepseek-chat # Choose whatever LLM you would like to use from the configured providers
```

** Using `INFER_A2A_ENABLED: true` automatically enables A2A tools (QueryAgent, QueryTask, SubmitTask) even when local tools
are disabled. This simplified configuration gives you only the A2A functionality without needing to configure each
tool individually.

Now you can enter the Interactive Chat within the cli container and start chatting:

```bash
docker compose run --rm cli
```

## Viewing Browser Agent GUI

The browser-agent can run in headed mode with VNC support for real-time viewing of browser automation.

**Requirements:**

1. Browser-agent v0.4.0+ with Xvfb support
2. `BROWSER_HEADLESS: false` environment variable set
3. Shared X11 socket volume between browser-agent and browser-vnc

**Usage:**

Connect to the VNC server on port 5900:

```bash
# Using a VNC client (e.g., RealVNC, TigerVNC, or macOS Screen Sharing)
# Connect to: localhost:5900
# Password: password
```

On macOS, you can use the built-in Screen Sharing app:

```bash
open vnc://localhost:5900
# When prompted, enter password: password
```

**Note:** The X11 display is created by Xvfb when the browser-agent container starts. The VNC server will connect
automatically and you can view browser automation in real-time.

## Troubleshooting

```bash
docker compose run --rm a2a-debugger tasks list
docker compose run --rm a2a-debugger tasks get <task_id>
docker compose run --rm a2a-debugger tasks submit-streaming "What's on my calendar today?"
```
