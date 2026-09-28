# mcp

**What** - the Model Context Protocol anti-corruption layer: connecting to MCP servers, discovering their tools, and exposing them to the agent.
**Why** - MCP servers speak their own protocol and their tools only exist at runtime, so a translating layer keeps
discovery and lifecycle out of the agent.
**How** - `infrastructure/` is a small HTTP client for MCP `2026-07-28`, and `Supervisor` manages the server
connections, their containers and their liveness.

## How it plugs in

- `Supervisor.DiscoverTools` registers each discovered tool as `MCP_<server>_<tool>`.
- Servers are configured in `.infer/mcp.yaml` or `~/.infer/mcp.yaml`. `infer mcp` and the `/mcp` shortcut manage them.
- Only tools are consumed. Embedded resource blocks in a tool result are flattened into text.

## Related

- [MCP Integration](../../../docs/mcp-integration.md)
- [Tools Reference](../../../docs/tools-reference.md)
