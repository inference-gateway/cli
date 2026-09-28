# tools

**What** - the tools bounded context: the registry of built-in tools, each tool's YAML manifest, and the loader for user-authored custom tools.
**Why** - the agent needs one place to ask "what is this tool, is it allowed in this mode, and does it
need approval?" without knowing which context implemented it.
**How** - each context builds its tools with `NewTools(...)`, registers them here, and the registry answers tool policy from the manifests.

## What it owns

- `registry.go` - registration, lookup and policy resolution for the built-in tools.
- `*.yaml` - one manifest per built-in tool: description, parameter schema, the agent `modes` it runs in, and `require_approval`.
- `custom/` (alias `customtools`) - loading custom tools from `~/.infer/tools/`, the project's
  `.infer/tools/` and `.agents/tools/`. A custom manifest is the built-in manifest plus `command`,
  `timeout` and `enabled`.
- `*_test.go` and the golden tool-definition fixture - the contracts the model sees.

## Why it is separate

Tools arrive from many contexts (file, bash, browser, computer, MCP, media, custom) but the agent
must see one uniform contract. The registry is that seam.

## How it plugs in

- Capability contexts call `NewTools(...)`, and `internal/container/container.go` registers the results.
- Custom tools are executed by running their manifest command with the call's arguments as JSON on stdin, and returning stdout.
- Configure built-ins under `tools.*` in `config.yaml` (env `INFER_TOOLS_*`), including per-tool `require_approval`.

## Related

- [Tools Reference](../../docs/tools-reference.md)
- [Custom Tools](../../docs/custom-tools.md)
- [Tool Approval](../../docs/tool-approval.md)
