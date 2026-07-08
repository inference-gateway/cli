# AGENTS.md

A README for coding agents working on the Inference Gateway CLI.

## Stack

- **Go 1.26**, module `github.com/inference-gateway/cli`. Entry point: `main.go` → `cmd.Execute()`.
- **Cobra** for CLI structure. Root subcommands share a dependency-injected service container (`internal/container/container.go`).
- Dev environment managed via **flox** (`.flox/env/manifest.toml` pins Go, `go-task`, `golangci-lint`, `gopls`, `pre-commit`, `ripgrep`, `markdownlint-cli`, `gh`). Run everything through `flox activate --`.

## Build / Test / Lint

```bash
task build                    # → ./infer binary
task run -- <args>            # go run . <args> (e.g. task run -- status)
task test                     # go test ./...
task test:coverage            # go test -cover ./...
task test:verbose             # go test -v ./...
go test ./internal/agent -run TestBashTool  # single test
task fmt                      # go fmt ./...
task lint                     # golangci-lint run + markdownlint fix
task vet                      # go vet ./...
task precommit:run            # all pre-commit hooks (MUST run before every push)
task mocks:generate           # regenerate counterfeiter fakes in tests/mocks/
task mod:tidy                 # go mod tidy
```

**IMPORTANT:** Always run `task precommit:run` before pushing any changes. This runs all pre-commit hooks (formatting, linting, vetting, tests) and ensures CI passes locally. Never skip this step.

## Architecture

The agent is an **event-driven state machine** (`internal/agent/agent_state_machine.go`). States flow: `Idle → CheckingQueue → StreamingLLM → PostStream → EvaluatingTools → ApprovingTools/ExecutingTools → PostToolExecution → CheckingQueue … → Completing → Idle`. Each state's executor lives in `internal/agent/states/<state>.go`.

**Domain/Infra split:**
- `internal/domain/` — pure interfaces and value types. `interfaces.go` is the central contract; touching it triggers mock regeneration in pre-commit.
- `internal/infra/` — adapters (SDK clients, storage backends), storage migrations.
- `internal/services/` — business logic (channels, scheduler, heartbeat, filewriter, skills).
- `internal/agent/tools/` — tool implementations. `registry.go` is the source of truth for registered tools.

**Two LLM client modes:** Gateway (default, HTTP via `inference-gateway/sdk`) vs Claude Code (shells out to `claude` CLI, subscription-based, Claude-only, no images).

## Testing

- Use Go's standard `testing` package. Colocate `_test.go` files with the package under test.
- **Mocks** use [counterfeiter](https://github.com/maxbrunsfeld/counterfeiter) and live in `tests/mocks/` — they are **committed** to the repo.
- If you add a new interface in `internal/domain`, add a `counterfeiter` line to `Taskfile.yml` under `mocks:generate` and run `task mocks:generate`.
- Prefer table-driven tests where inputs and expected results vary.
- SA5011 false positives in tests are suppressed in `.golangci.yml` — `t.Fatal` is recognised as no-return.

## Linter Constraints

`.golangci.yml` enforces:
- `gocyclo`/`cyclop` max **25**
- `funlen` max **150 lines / 80 statements**
- `gocognit` max **45**
- `nestif` min-complexity **4**

Use `//nolint:funlen,gocyclo,cyclop` on long-but-cohesive functions rather than splitting them. Disabled linters: `exhaustruct`, `varnamelen`, `wrapcheck`, `paralleltest`, `testpackage`.

## Code & Commit Style

- **Conventional Commits** (`.commitlintrc.json`): `feat:`, `fix:`, `docs:`, `style:`, `refactor:`, `perf:`, `test:`, `build:`, `ci:`, `chore:`, `revert:`.
- `.editorconfig`: UTF-8, LF endings, final newline, two-space indent (tabs for Go files).
- Package names are short, lowercase, descriptive.

## Configuration

Config is **split across multiple YAML files** under `.infer/` (project) and `~/.infer/` (userspace):

| File | Purpose |
|------|---------|
| `config.yaml` | gateway, storage, tools, agent, chat, web, pricing |
| `prompts.yaml` | LLM system prompts + per-tool descriptions |
| `agents.yaml` | A2A agent registry |
| `keybindings.yaml` | TUI keybindings |
| `channels.yaml` | Telegram channel config |
| `heartbeat.yaml` | Periodic wake-up config |
| `mcp.yaml` | MCP server registry |
| `computer_use.yaml` | Mouse/keyboard/screenshot settings |
| `shortcuts/*.yaml` | Custom `/`-prefixed chat commands |

Env var override format: `INFER_<PATH_WITH_UNDERSCORES>` (e.g. `INFER_AGENT_MODEL`).

**After editing config defaults**: run `go run . init --overwrite`, then **restore `agents.yaml`** (`git checkout -- .infer/agents.yaml`) — it contains user-curated A2A registrations and `init --overwrite` nukes it. Same caution applies to `mcp.yaml`, `channels.yaml`, `computer_use.yaml`, `heartbeat.yaml`.

## Security Gotchas

- **Bash allow-list is default-deny.** Anything not matched is blocked (headless) or sent to approval (chat). The allow-list is **per agent mode** under `tools.bash.mode.{all,plan,standard,auto}.allow`. The effective list for a mode = `mode.all.allow` (baseline) ∪ that mode's own entries. By default, only `mode.auto` (YOLO mode, shift+tab in chat) carries `.*` (unrestricted). Standard (headless default) and Plan are read-only.
- **Tool approval is two-layer:** `tools.safety.require_approval` decides *whether* approval is needed; `tools.safety.approval_behaviour` (`prompt` | `ipc` | `block`) decides *how*. Headless mode blocks by default when no approver is reachable.
- Never commit real secrets. Use `.env` for credentials; `.env.example` as a template.
- `BackgroundTaskRegistry` is the **single owner** of both A2A task tracking and background bash shell tracking. Don't construct them separately.
- Plan mode is enforced by tool filtering (`FilterToolsForMode`), not by the agent. Plans persist as Markdown under `.infer/plans/`.
- Conversation storage failure on init panics (rather than silently falling back) — set `storage.enabled: false` to opt out.
