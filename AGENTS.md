# AGENTS.md

A README for coding agents working on the Inference Gateway CLI.

## Stack

- **Go 1.26**, module `github.com/inference-gateway/cli`. Entry point: `main.go` → `cmd.Execute()`.
- **Cobra** for CLI structure. Root subcommands share a dependency-injected service container (`internal/container/container.go`).
- Dev environment managed via **flox** (`.flox/env/manifest.toml` pins Go, `go-task`, `golangci-lint`, `gopls`, `ripgrep`, `markdownlint-cli`, `gh`). Run everything through `flox activate --`.

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
task precommit:run            # the pre-commit hook: .githooks/pre-commit (MUST run before every push)
task mocks:generate           # regenerate counterfeiter fakes in tests/mocks/
task mod:tidy                 # go mod tidy
```

**IMPORTANT:** Always run `task precommit:run` before pushing any changes. This runs the pre-commit hook (formatting, linting, mod-tidy, mocks) and ensures CI passes locally. Never skip this step.

## Architecture

The agent is an **event-driven state machine** (`internal/agent/agent_state_machine.go`). States flow: `Idle → CheckingQueue → StreamingLLM → PostStream → EvaluatingTools → ApprovingTools/ExecutingTools → PostToolExecution → CheckingQueue … → Completing → Idle`. Each state's executor lives in `internal/agent/states/<state>.go`.

**Bounded contexts (DDD — contexts first, layers second).** There is no central domain package; each context owns its contracts in a pure, stdlib-plus-sdk-only `domain/` subpackage, and touching any `*/domain` package triggers mock regeneration in the pre-commit hook:
- `internal/agent/` — the core domain. `agent/domain/` is the shared kernel (Tool contract, tool-call value types, chat events, hooks, frames/annotations, service ports like `FileService`/`MCPManager`/`SkillsService`); `agent/states/` holds the state-machine contracts and per-state executors; `agent/application/` holds adk-coupled A2A contracts and `agentrunner`; `agent/infrastructure/` holds the lipgloss tool formatter. `agent/tools/registry.go` is the source of truth for registered tools; capability packages register theirs via `Registry.RegisterTools`.
- `internal/conversation/` — conversation context: `domain/` (ConversationEntry, repository/optimizer/model/pricing/queue contracts, sessions) + application services (persistent/in-memory repos, tokenizer, rollover, title generation, event bridge).
- `internal/scheduler/` — background-work context: `domain/` (scheduled jobs, background jobs, shell/subagent tracking, task retention) + cron scheduler, job supervisor (`jobs/`), `githubscheduler/`, `heartbeat/`.
- `internal/browser/`, `internal/computer/` — capabilities plugged into the agent through the `agentdomain.Tool` contract, each with a pure `domain/` and an `infrastructure/` (Playwright only under `browser/infrastructure`; robotgo and display backends only under `computer/infrastructure`). They never import `agent/tools`, `services`, or `ui`.
- `internal/audio/` — capability: recording, conversion, and whisper.cpp speech-to-text.
- `internal/platform/` — shared platform layer: `logger`, `constants`, `formatting`, `telemetry`, `utils`, `project`, `models`, `streamevent`, `render`, `storage` (+migrations), `memory`, `adapters`, `ipc`, `container` (docker/podman runtime contract).
- `internal/services/` — remaining application services (channels, MCP manager, filewriter, skills) pending the presentation regroup (issue #1087 phases 6–7).
- `internal/ui/` — Bubble Tea presentation: `ApplicationState`, view/manager contracts, UI events, theming. Only presentation packages may import it.

Import direction: `*/domain` packages import nothing internal (agent/domain is the bottom); other domains may import `agent/domain`; capabilities import domains + platform only; `services` sits above domains and `ui`; `container` and `cmd` compose everything.

## Testing

- Use Go's standard `testing` package. Colocate `_test.go` files with the package under test.
- **Mocks** use [counterfeiter](https://github.com/maxbrunsfeld/counterfeiter) and live in `tests/mocks/` — they are **committed** to the repo.
- If you add a new interface in a context's `domain/` package, add a `counterfeiter` line to `Taskfile.yml` under `mocks:generate` and run `task mocks:generate`.
- Prefer table-driven tests where inputs and expected results vary.
- SA5011 false positives in tests are suppressed in `.golangci.yml` — `t.Fatal` is recognised as no-return.

### Manual testing against real services

**Never start the gateway or A2A agent containers by hand** (`docker run
ghcr.io/inference-gateway/inference-gateway`, manual `docker pull`, etc.) when
manually testing the CLI. `go run . chat` and `go run . agent <prompt>` are
self-contained: the CLI auto-starts the local gateway and every `run: true`
agent from `agents.yaml` (pulling images as needed) and tears them down on
session end. Just run the command; the only manual inputs are the model flag
(`-m provider/model`) and any `INFER_*` env overrides.

### Driving the chat TUI via tmux

The chat TUI (Bubble Tea v2) can be exercised end-to-end by running it inside a
tmux session and scripting it with `send-keys` / `capture-pane`. The generic tmux
mechanics — session/pane lifecycle, `send-keys -l` for literal text vs named keys,
sleep-before-`capture-pane`, cleanup discipline — are the single source of truth in
the built-in **`tmux` skill** (`internal/services/skills/builtins/tmux/SKILL.md`);
`tests/e2e/tmux_tui_test.go` drives the TUI the same way. This section only covers
the repo-specific glue.

Run the TUI against the embedded mock gateway (`INFER_GATEWAY_MOCK=true`) so no
real LLM is called. This scripted recipe uses a **detached** session on purpose
(deterministic captures, no attached viewer, works in CI). When you are driving a
TUI interactively for someone to watch, do the opposite — follow the `tmux` skill
and split a pane in the **current** session instead of starting a new one.

```bash
tmux new-session -d -s infer-tui -x 200 -y 50 \
  'INFER_GATEWAY_MOCK=true go run . chat'
```

- Wait ~3s for `go run` to compile and the TUI to render before the first capture.
- The mock advertises one model (`openai/gpt-4o`); once the picker renders, `Enter`
  selects it. Then send a prompt (`send-keys -t infer-tui -l 'say hello'`, then
  `Enter` as a separate call) and read it back with
  `capture-pane -t infer-tui -p -S -50`.
- `BTab` (shift+tab) toggles agent mode.

**Mock gateway scenarios:** the mock matches the latest real user message
(injected `<system-reminder>` content is skipped) against the regexes in the
embedded scenario library (`github.com/inference-gateway/tokenless/gateway`)
— e.g. `say hello` → a text reply, `please search for X` → a Grep tool call.
Unmatched prompts get the `Done.` fallback. To test with custom scenarios, set
`INFER_GATEWAY_MOCK_SCENARIOS=/path/to/scenarios.yaml` — the container loads
it via `mockgateway.LoadFile` and serves it on the mock gateway. The
`tests/e2e/` and `tests/integration/` packages import the tokenless harness
(`github.com/inference-gateway/tokenless/harness`) for in-process mock setup
and the gateway library for scenario definitions and request inspection.

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
| `browser_use.yaml` | Browser automation (Playwright) settings |
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
