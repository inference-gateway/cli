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

** Using `INFER_A2A_ENABLED: true` automatically enables A2A tools (QueryAgent, QueryTask, Task) even when local tools
are disabled. This simplified configuration gives you only the A2A functionality without needing to configure each
tool individually.

Now you can enter the Interactive Chat within the cli container and start chatting:

```bash
docker compose run --rm cli
```

## Troubleshooting

```bash
docker compose run --rm a2a-debugger tasks list
docker compose run --rm a2a-debugger tasks get <task_id>
docker compose run --rm a2a-debugger tasks submit-streaming "What's on my calendar today?"
```
