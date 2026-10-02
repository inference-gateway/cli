# AGENTS.md

Agent rules for `internal/tools`. They take precedence over the root `AGENTS.md` for files in this directory.

- Add a built-in tool in three places: its YAML manifest, its `Tool*` constant in `tool_manifests.go`, and its
  `r.register(...)` line in `registerTools`. Keep the manifest and the constructor in sync.
- Refer to a tool name through the owning context's `Tool*` constant, anywhere in the codebase. Never write the name as a string literal.
- Custom tools never take a built-in name (each context's `ToolNames()`).
- The file tools never write into the custom-tool directories (`sandbox.ValidateWrite`).
- A registry change must be reflected in the golden tool definitions (`internal/loop/testdata/tool_definitions.golden.json`).
