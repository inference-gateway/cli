# Inference Gateway CLI Examples

This directory contains practical examples for using the `infer` CLI tool to interact with the Inference Gateway.

## Prerequisites

1. Configure the Inference Gateway server:

```bash
# Configure the providers you want to work with
cp .env.gateway.example .env.gateway
```

2. Configure the Google Calendar Agent:

```bash
# Configure the Google Calendar A2A Server Agent
cp .env.agent.example .env.agent
```

3. Bring all the containers up:

```bash
docker compose up -d
```

4. Log the containers verify that everything is up and running:

```bash
docker compose ps
docker compose logs -f
```

## Configuration

Set up your CLI configuration via environment variables (review docker-compose.yaml infer-cli service):

```yaml
INFER_GATEWAY_URL: http://inference-gateway:8080
INFER_GATEWAY_MIDDLEWARES_A2A: true
INFER_TOOLS_ENABLED: false
INFER_AGENT_MODEL: deepseek/deepseek-chat # Choose whatever LLM you would like to use from the configured providers
```

** Disabled local tools to save some costs, since you only want to see that it works with the A2A - feel free to
enable them if you want they will get merged with the Inference Gateway A2A related tools.

Now you can enter the Interactive Chat within the infer-cli container and start chatting:

```bash
docker compose run --rm infer-cli
```

## Troubleshooting

```bash
docker compose run --rm a2a-debugger tasks list
docker compose run --rm a2a-debugger tasks get <task_id>
docker compose run --rm a2a-debugger tasks submit-streaming "What's on my calendar today?"
```
