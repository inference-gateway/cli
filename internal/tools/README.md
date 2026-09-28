# tools

**What** - the tools bounded context: the registry of built-in tools, each tool's YAML manifest, and the loader for user-authored custom tools.
**Why** - the agent needs one place to ask "what is this tool, is it allowed in this mode, and does it
need approval?" without knowing which context implemented it.
**How** - each context builds its tools with `NewTools(...)`, the container registers them here, and the registry
answers tool policy from the manifests.

## How it plugs in

- A manifest carries the description, the parameter schema, the agent `modes` a tool runs in, and `require_approval`.
- `custom/` (alias `customtools`) loads custom tools from `~/.infer/tools/`, the project's `.infer/tools/` and
  `.agents/tools/`. A custom manifest adds `command`, `timeout` and `enabled`, and the tool gets the call's
  arguments as JSON on stdin and returns its stdout.
- MCP tools are registered at runtime from `Supervisor.DiscoverTools`.
- Configure the built-ins under `tools.*` in `config.yaml` (env `INFER_TOOLS_*`).

## Related

- [Tools Reference](../../docs/tools-reference.md)
- [Custom Tools](../../docs/custom-tools.md)
- [Tool Approval](../../docs/tool-approval.md)
