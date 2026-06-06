# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Dev environment

This repo expects [flox](https://flox.dev) (`.flox/env/manifest.toml` pins Go 1.26, `go-task`, `golangci-lint`, `gopls`, `pre-commit`, `ripgrep`, `markdownlint-cli`, `gh`). Run everything through `flox activate --`:

```bash
flox activate -- task build           # → ./infer (ldflags inject version/commit/date into cmd/version_info.go)
flox activate -- task test            # go test ./...
flox activate -- task lint            # golangci-lint run + markdownlint
flox activate -- task fmt             # go fmt ./...
flox activate -- task precommit:run   # all hooks (fmt, lint, mod-tidy, mocks)
flox activate -- task mocks:generate  # regenerate counterfeiter fakes under tests/mocks/
flox activate -- task run CLI_ARGS="chat"  # equivalent to ./infer chat without building
```

A single test: `flox activate -- go test ./internal/agent/tools -run TestBashTool`.

`task mocks:generate` is wired into pre-commit and fires automatically when `internal/domain/interfaces.go` changes. If you add a new interface in `internal/domain`, also add a `counterfeiter` line to `Taskfile.yml` under `mocks:generate` — otherwise the fake won't exist and tests that depend on it won't compile.

## Linter constraints

`.golangci.yml` enforces complexity ceilings that are higher than golangci-lint defaults but still bite on large functions: `gocyclo`/`cyclop` max 25, `funlen` 150 lines / 80 statements, `gocognit` 45, `nestif` 4. When a long-but-cohesive function legitimately needs to exceed them (e.g. `initializeProject` in `cmd/init.go`, `StartChatSession` in `cmd/chat.go`), use a targeted `//nolint:funlen,gocyclo,cyclop` rather than splintering the flow.

Commits use Conventional Commits (`.commitlintrc.json`); release tooling (semantic-release on `main`) maps `feat:` → minor, `fix:` → patch, `feat!:` / `BREAKING CHANGE:` → major.

## Big-picture architecture

The CLI is a Cobra app whose root subcommands all share one dependency-injected service container.

**Entry points** (`main.go` → `cmd/root.go`): `initConfig()` (in `cmd/root.go`) layers config in this order — built-in defaults → `~/.infer/config.yaml` → `./.infer/config.yaml` → flags → `INFER_*` env vars (highest). Three env vars get special list-parsing because viper can't handle them generically: `INFER_A2A_AGENTS`, `INFER_TOOLS_BASH_WHITELIST_COMMANDS`, `INFER_TOOLS_BASH_WHITELIST_PATTERNS` (split on `,` or newline). The two whitelist vars **replace** the resolved list; their `_APPEND` siblings — `INFER_TOOLS_BASH_WHITELIST_COMMANDS_APPEND` and `INFER_TOOLS_BASH_WHITELIST_PATTERNS_APPEND` — instead **merge onto** the fully-resolved default/config/replace list (applied after `ReadInConfig`), so callers can add per-repo entries without clobbering the built-in default. The CLI default is the single source of truth for the whitelist; consumers like `infer-action` and the org reusable workflow are pass-throughs that only forward these vars. The resolved `*config.Config` is stored on package-level `cmd.Cfg` and read directly by every subcommand — do not re-unmarshal viper in command code.

**Service container** (`internal/container/container.go`): `NewServiceContainer(cfg)` wires every long-lived dependency in a fixed order — gateway manager → state manager → domain services (which also constructs the tool registry, MCP manager, conversation storage backend, agent state machine, optimizer) → agent manager (only if A2A enabled) → background services → UI components → extensibility (shortcuts). The container is the single source of truth for service identity; if you need a new service available from a Cobra command, add a `Get*()` accessor here rather than constructing it ad-hoc.

**Two runtime modes for the LLM client** (selected by config, see `createAgentSDKClient` in `container.go`):
- **Gateway mode** (default): HTTP via `inference-gateway/sdk` against a gateway URL. The container can auto-start the gateway binary or container itself when `gateway.run: true`.
- **Claude Code mode** (`claude_code.enabled: true`): shells out to the `claude` CLI via `internal/infra/adapters/claude_code_client.go` and adapts its streaming output to `domain.SDKClient`. Subscription-based, Claude-only, no images/prompt-caching.

**Agent core** (`internal/agent/`): the agent is an **event-driven state machine**, not a linear loop.
- `agent.go` — `AgentServiceImpl` owns per-request streaming/cancellation, per-session cancellation (one `sessionCancel` cancels streaming, tool execution, approval waits, pollers, and the main loop via a single `sync.Once`), tool-call accumulation, and a cached git context.
- `agent_state_machine.go` — registers transitions between `domain.AgentExecutionState` values. State flow: `Idle → CheckingQueue → StreamingLLM → PostStream → EvaluatingTools → ApprovingTools/ExecutingTools → PostToolExecution → CheckingQueue …  → Completing → Idle`. Each state's `Execute` method lives in `internal/agent/states/<state>.go`. To add a new state: add a constant in `internal/domain/state.go`, add transitions in `agent_state_machine.go::registerTransitions`, and create the executor file.
- `agent_event_driven.go` / `agent_streaming.go` — bridge SDK SSE events to internal `domain.ChatEvent`s (consumed by the TUI).

**Tools** (`internal/agent/tools/`): each tool implements `domain.Tool` (`Definition`, `Execute`, `Validate`, `IsEnabled`). `Registry` (`registry.go`) registers the always-on set in `registerTools()`, gates optional tools on config (`Schedule`, `WebFetch`, `WebSearch`, `Github`, A2A trio, computer-use suite, background-shell trio). MCP tools are **not** discovered at construction time — they're registered async via `RegisterMCPServerTools` from the MCP manager's liveness probe (see comment block at top of `registry.go` and issue #523). When adding a tool: implement the interface, register in `registerTools()`, add config struct + defaults in `config/config.go`, write a `_test.go` next to it; if the tool mutates state, also add it to the approval policy.

**Domain ↔ Infra split**:
- `internal/domain/` — pure interfaces and value types. `interfaces.go` is the central contract; touching it triggers a mock regeneration in pre-commit.
- `internal/infra/storage/` — pluggable conversation backends (`jsonl` default, `sqlite` via `modernc.org/sqlite` — pure Go, no CGO — `postgres`, `redis`, `memory`). Selected via `storage.type` in config; factory in `factory.go`. SQLite/Postgres run migrations from `internal/infra/storage/migrations/`.
- `internal/infra/adapters/` — concrete adapters bridging external SDKs to domain interfaces (`sdk_client_adapter.go`, `claude_code_client.go`, `persistent_conversation_adapter.go`).
- `internal/services/` — business logic implementing domain interfaces. Subpackages: `channels/` (Telegram), `scheduler/`, `heartbeat/`, `middleware/` (approval), `filewriter/`, `skills/`.

**UI** (Bubble Tea TUI): `internal/app/chat.go::ChatApplication` is the root tea.Model. Domain events flow into the UI via `internal/handlers/` (`chat_handler.go`, `chat_message_processor.go`, `chat_shortcut_handler.go`). UI components in `internal/ui/components/`. Theme, autocomplete, keybindings, and history each have a subpackage under `internal/ui/`. There is also a **web terminal** mode (`infer chat --web`) that runs a PTY-backed multi-tab terminal server from `internal/web/`.

**Daemon mode** (`infer channels-manager`, in `cmd/channels.go`): one long-running process that hosts up to three independent subsystems — channels (Telegram inbound polling), scheduler (cron-driven), heartbeat (periodic agent wake-up). At least one must be enabled or the daemon refuses to boot. Each subsystem **spawns `infer agent` as a subprocess** rather than calling the agent in-process — this means session state (per-sender for channels, per-fire UUID for scheduler/heartbeat) survives across runs via the storage backend. The scheduler watches `~/.infer/schedules/<uuid>.yaml` with fsnotify and hot-reloads; cron expressions use `time.Local` and the binary embeds `time/tzdata` (see `main.go`) so `TZ=Europe/Berlin` works in minimal containers.

**Tool approval flow over channels**: when the agent runs as a subprocess under channels-manager with `--require-approval`, approval is brokered over **stdin/stdout JSON IPC** (`internal/domain/ipc.go`). Agent writes `ApprovalRequest` to stdout, blocks on stdin; channel manager forwards to the user, intercepts the reply in `routeInbound()` (before `handleMessage` — important, otherwise sender mutex deadlocks), writes back `ApprovalResponse`. 5-minute auto-reject timeout.

## Configuration layout

User-visible config lives in `.infer/` (project) and/or `~/.infer/` (userspace), seeded by `infer init`. It is **split across multiple YAML files by concern**, not one monolithic file:

| File | Purpose |
| --- | --- |
| `config.yaml` | gateway, storage, tools, agent, chat, web, pricing |
| `prompts.yaml` | LLM system prompts + per-tool descriptions (`tools.<ToolName>.description`) |
| `keybindings.yaml` | chat TUI keybindings |
| `channels.yaml` | Telegram etc. + per-channel allowlists |
| `heartbeat.yaml` | wake-up interval, prompt, model override |
| `computer_use.yaml` | mouse/keyboard/screenshot settings |
| `agents.yaml` | A2A agent registry |
| `mcp.yaml` | MCP server registry |
| `shortcuts/*.yaml` | `/`-prefixed chat commands (git, scm, mcp, shells, export, a2a, skills) |

`Config` struct in `config/config.go` marks the split-out files with `yaml:"-" mapstructure:"-"` (`ComputerUse`, `Channels`, `Heartbeat`, `Prompts`) because viper only reads `config.yaml`; the dedicated loaders for the others live alongside their config structs in `config/`.

Env var override format: `INFER_<PATH_WITH_UNDERSCORES>` (dots → underscores). Example: `agent.model` → `INFER_AGENT_MODEL`.

## Things to know that aren't obvious from the code

- **Binary name is `infer`, module is `cli`**: `go install` produces `cli`; the Taskfile and Nix flake rename it to `infer`. macOS Nix builds also compile the Swift computer-use bridge under `internal/display/macos/ComputerUse/`.
- **Tools were moved** from `internal/services/tools/` to `internal/agent/tools/`. CONTRIBUTING.md still has the old path in places — trust `internal/agent/tools/registry.go` over the doc.
- **`BackgroundTaskRegistry` is the single owner** of both A2A task tracking and background-bash-shell tracking. `domain.A2ATaskTracker` and `domain.ShellTracker` are narrower projections of the same instance; don't construct them separately or you'll observe diverging state.
- **Plan mode is enforced by tool filtering**, not by the agent. `internal/services/tools.go::FilterToolsForMode` strips mutating tools when the agent is in `AgentModePlan` and exposes `RequestPlanApproval` instead. Plans persist as Markdown under `.infer/plans/<timestamp>-<slug>.md` (atomic write); rejected plans are kept as audit trail.
- **`Schedule` tool routing is deterministic**, never LLM-guessed. The agent injects its session ID into the tool's context (`domain.WithSessionID`); `Schedule.resolveRouting` parses channel/recipient from the session ID format `channel-<name>-<sender_id>` and refuses to run from a non-channel session (e.g. chat or heartbeat).
- **Conversation persistence requires storage `enabled: true`**. If `enabled: true` and the configured backend fails to initialize, the container **panics** with a clear "fix config or set storage.enabled: false" message rather than silently falling back — see `handleStorageInitFailure` in `container.go`.
- **Counterfeiter mocks are committed** under `tests/mocks/`. Regenerate via `task mocks:generate` (pre-commit handles this when `internal/domain/interfaces.go` changes, but you may need it manually after changing other listed interface files — see the `sources:` list under `mocks:generate` in `Taskfile.yml`).
