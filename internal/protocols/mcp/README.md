# mcp

**What** - the Model Context Protocol integration: connecting to MCP servers, discovering their tools, and exposing them to the agent.
**Why** - MCP servers speak their own protocol and their tools appear at runtime, so a translating layer keeps discovery and lifecycle out of the agent.
**How** - server connections are configured in `mcp.yaml`, and each discovered tool is registered in the tools registry as `MCP_<server>_<tool>`.

## What it owns

- The MCP client and connection lifecycle (start, health, reconnect, shutdown).
- Tool discovery and the dynamic registration of `MCP_<server>_<tool>` tools.
- `mcp add|remove|enable|disable` support and the gateway's `/mcp` entry point that fronts all of its servers.
- Resource and prompt exposure where a server offers them.

## Why it is separate

MCP is an external protocol with its own SDK and versioning. The CLI consumes it the same way it
consumes A2A: as a protocol integration under `internal/protocols/`, never as a direct dependency of
the agent loop or the TUI.

## How it plugs in

- Configure servers in `.infer/mcp.yaml` or `~/.infer/mcp.yaml`; `/mcp` and `infer mcp` manage them.
- MCP tools are not listed statically in the tools reference because they only exist once a server is connected.
- Servers must speak MCP `2026-07-28`, and the gateway's `/mcp` endpoint works as a single entry for all of its servers.

## Related

- [MCP Integration](../../../docs/mcp-integration.md)
- [Tools Reference](../../../docs/tools-reference.md)
- [Configuration Reference](../../../docs/configuration-reference.md)
