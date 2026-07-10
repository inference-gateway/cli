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

## Testing

- Use Go's standard `testing` package. Colocate `_test.go` files with the package under test.
- **Mocks** use [counterfeiter](https://github.com/maxbrunsfeld/counterfeiter) and live in `tests/mocks/` — they are **committed** to the repo.
- If you add a new interface in `internal/domain`, add a `counterfeiter` line to `Taskfile.yml` under `mocks:generate` and run `task mocks:generate`.
- Prefer table-driven tests where inputs and expected results vary.
- SA5011 false positives in tests are suppressed in `.golangci.yml` — `t.Fatal` is recognised as no-return.

### Driving the chat TUI via tmux

The chat TUI (Bubble Tea v2) can be exercised end-to-end without a physical
keyboard by running it inside a detached tmux session and scripting it with
`send-keys` / `capture-pane`. Combine with the embedded mock gateway
(`INFER_GATEWAY_MOCK=true`) so no real LLM is called.

**1. Start a dedicated session** (fixed size makes captures deterministic):

```bash
tmux kill-session -t infer-tui 2>/dev/null || true
tmux new-session -d -s infer-tui -x 200 -y 50 \
  'INFER_GATEWAY_MOCK=true go run . chat'
```

**2. Select a model** — the mock advertises a single model (`openai/gpt-4o`),
so once the picker renders, Enter selects it:

```bash
sleep 3   # `go run` compiles first; wait for the TUI to render
tmux send-keys -t infer-tui Enter
```

**3. Type and submit a prompt.** Use `-l` for literal text (otherwise tmux
interprets words like `Enter` or `Space` as key names), then send Enter as a
separate call:

```bash
tmux send-keys -t infer-tui -l 'say hello'
tmux send-keys -t infer-tui Enter
```

Special keys use tmux key names, not `-l`: `Enter`, `Escape`, `Tab`, `BTab`
(shift+tab, toggles agent mode), `Up`/`Down`, `C-c`.

**4. Capture and assert.** Always sleep briefly before capturing — the TUI
repaints asynchronously and an immediate capture shows a stale frame:

```bash
sleep 1
tmux capture-pane -t infer-tui -p -S -50
```

`-S -50` includes scrollback; grep the output for the expected response.

**5. Clean up** when done (also kills the CLI process):

```bash
tmux kill-session -t infer-tui
```

**Mock gateway scenarios:** the mock matches the latest real user message
(injected `<system-reminder>` content is skipped) against the regexes in
`internal/mockgateway/scenarios.yaml` — e.g. `say hello` → a text reply,
`please search for X` → a Grep tool call. Unmatched prompts get the `Done.`
fallback. To test with custom scenarios, build the standalone binary
(`task build:mockgateway` → `.infer/bin/mock-gateway --scenarios my.yaml`),
read the listen address from its first stdout line, and point the CLI at it
with `INFER_GATEWAY_URL` instead of `INFER_GATEWAY_MOCK`.

**Pitfalls:**

- `send-keys` without `-l` treats semicolons as command separators and
  capitalised words as key names — always use `-l` for message text.
- Target the session by name (`-t infer-tui`); pane IDs like `%16` are not
  stable across runs.
- If a run wedges the TUI, `tmux send-keys -t infer-tui C-c` then
  `kill-session` — don't leave orphaned sessions behind.

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
