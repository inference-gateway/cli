# AGENTS.md

Agent rules for `internal/tools`. They take precedence over the root `AGENTS.md` for files in this directory.

- `registry.go` is the source of truth for the registered built-in tools. Register a tool by adding its manifest and its constructor, never by editing callers.
- Every built-in tool has a YAML manifest next to the Go code: description, parameter schema, the agent `modes` it runs in, and `require_approval`. Keep the manifest and the constructor in sync - the registry answers tool policy from the manifest.
- Refer to a tool name through the owning context's `Tool*` constant. Never write the name as a string literal at a call site.
- Custom tools (`custom/`, alias `customtools`) load the same manifest plus `command`, `timeout` and `enabled` from `~/.infer/tools/`, the project's `.infer/tools/` and `.agents/tools/`. They never take a built-in name (each context's `ToolNames()`).
- The file tools never write into the custom-tool directories (`Config.ValidatePathInSandboxWrite`).
- A registry change must be reflected in the golden tool definitions (`internal/agent/testdata/tool_definitions.golden.json`).
