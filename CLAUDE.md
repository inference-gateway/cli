# CLAUDE.md

## Project: Inference Gateway CLI (`infer`)

Go CLI for ML inference services with TUI chat, status monitoring, and config management.

## Dev Commands

All commands: `flox activate -- task <command>`

**Setup**: `task setup` (run first)
**Config**: `task run -- config init --overwrite` (generate .infer/config.yaml)
**Build**: `task build`
**Test**: `task test` / `test:verbose` / `test:coverage`
**Run**: `task run -- [args]` / `run:status` / `run:version`
**Quality**: `task fmt` / `lint` / `vet` / `check` / `dev`
**Modules**: `task mod:tidy` / `mod:download`
**Release**: `task release:build` / `clean:release`

## Architecture

```go
main.go → cmd/Execute()
cmd/: CLI commands (root, config, status, chat, version)
config/: YAML configuration
internal/:
  app/: Bubble Tea models
  handlers/: Request handlers
  services/: Business logic
  ui/: UI components
  domain/: Models & interfaces
  container/: Dependency injection
```

## Config (.infer/config.yaml)

- **gateway**: url, api_key, timeout
- **chat**: default_model, system_prompt
- **tools**: enabled, sandbox dirs, protected paths, safety settings
- **compact**: output_dir
- **web_search**: enabled, engine, max_results

## Commands

- `infer init [--overwrite]`: Initialize config
- `infer config set-model MODEL`: Set default model
- `infer config set-system PROMPT`: Set system prompt
- `infer config tools [enable|disable|list|validate|exec]`
- `infer config tools safety [enable|disable|status|set|unset]`
- `infer config tools sandbox [list|add|remove]`
- `infer chat`: Interactive chat (with token tracking)
- `infer status`: Gateway status

## Chat Features

- Model selection (or uses default if configured)
- Scrollable history (↑↓/k/j, PgUp/PgDn, Home/End)
- Token tracking (per-request & session totals)
- Commands: `/clear`, `/compact`, `/exit`
- Bash mode: `!command` for direct shell execution
- Tools mode: `!!ToolName(arg="value")` for direct tool execution

## Tools Mode

Access tools directly in chat with `!!` prefix:

- `!!Read(file_path="path/to/file")`: Read file contents
- `!!Write(file_path="path", content="text")`: Write to file
- `!!Bash(command="ls -la")`: Execute bash command
- `!!WebSearch(query="search term")`: Search the web
- `!!Tree()`: Show directory tree
- `!!Grep(pattern="text", path=".")`: Search in files

Features:

- Tab autocompletion for available tools
- Real-time tool execution in chat context
- Structured argument parsing: `key="value"` format
- Error handling and validation

## Security

- Tool approval prompts (configurable)
- Command whitelisting
- Sandbox directories
- Protected paths (.infer/, .git/, *.env)

## Code Style

- No inline comments unless necessary
- Follow existing patterns
- Check deps before using
- Commit format: `type: Capitalize description`
- Never use "enhance" in commits

## Dependencies

Cobra, Bubble Tea, YAML v3, Go 1.24+

## Token Optimization

- Use chat_export_* files: Read only "## Summary" to "---" section
- Apply code directly with tools (no examples in responses)
- Plan with TodoWrite, mark progress immediately
- Batch tool calls for efficiency
