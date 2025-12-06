# MCP Demo Server

A demonstration Model Context Protocol (MCP) server with four example tools: `get_time`, `calculate`, `list_files`, and `get_env`.

## Quick Start

```bash
# Copy and configure API keys
cp .env.example .env
# Edit .env and add your DEEPSEEK_API_KEY, OPENAI_API_KEY, or ANTHROPIC_API_KEY

# Start services
docker compose up --build

# In another terminal, run the CLI
docker compose run --rm cli
```

The CLI will automatically discover the MCP tools and you can start chatting!

## Example Prompts

Try these with the LLM:

- "What time is it in Tokyo?"
- "Calculate 123 + 456"
- "List all Go files in the current directory"
- "What's the DEMO_MESSAGE environment variable?"

## Services

- **inference-gateway**: Inference Gateway for LLM access
- **mcp-demo-server**: MCP server with example tools (see `mcp-server/`)
- **cli**: Inference Gateway CLI using `ghcr.io/inference-gateway/cli:latest`

## Troubleshooting

### View detailed logs

```bash
docker compose logs -f
```

### Reset everything

```bash
docker compose down -v
```

## Configuration

The MCP configuration is in `mcp-config.yaml` and mounted into the CLI container.

For more details, see the [MCP Integration Guide](../../docs/mcp-integration.md).
