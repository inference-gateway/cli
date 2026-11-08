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

## Agents That Pick Their MCP Servers

`agents/` holds two Markdown agents, mounted into the CLI as `~/.infer/agents/`. Each one names the MCP servers its
subagents bring up with `mcpServers`:

- `timekeeper` lists `demo-server`, so it gets the demo server's tools and no other server.
- `summarizer` lists none (`mcpServers: []`), so it connects to no MCP server at all.

Try them from the chat:

- "Ask the timekeeper agent what time it is in Tokyo"
- "Have the summarizer agent summarize this README"

An agent without `mcpServers` gets every enabled server in `mcp-config.yaml`. A server set to `enabled: false` there
stays off for the chat but still comes up for an agent that names it. See
[Markdown Subagents](../../docs/subagents.md#mcp-servers).

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

The MCP configuration is in `mcp-config.yaml` and mounted into the CLI container, the agents in `agents/` next to it.

For more details, see the [MCP Integration Guide](../../docs/mcp-integration.md).
