# AGENTS.md

A README for coding agents on the **Inference Gateway CLI** — an agentic CLI (chat TUI, headless agent, A2A agents, tool execution). User-facing docs live in README.md and docs/.

## Stack

- Go 1.26, module `github.com/inference-gateway/cli`. Entry: `cmd/infer/main.go` → `root.Execute()` (Cobra). `internal/container/container.go` is the DI composition root.
- Dev env pinned by **flox** (`.flox/env/manifest.toml`); run everything through `flox activate --`.

## Build / Test / Lint

```bash
task build              # → ./infer binary
task run -- <args>      # go run ./cmd/infer <args>
task test               # go test ./...; variants: test:verbose, test:coverage, test:race, test:e2e
task fmt                # gofmt + gci
task vet                # go vet
task lint               # lint:imports → golangci-lint → markdownlint
task mocks:generate     # counterfeiter fakes → tests/mocks/
task precommit:run      # .githooks/pre-commit: mod:tidy → mocks → fmt → lint
```

Single test: `go test ./internal/agent -run TestBashTool`. **Run `task precommit:run` before every push** — it's the CI gate, and it aborts if `task fmt` reformats your staged files (re-`git add` and retry).

## Architecture

- The agent is an **event-driven state machine** (`internal/agent/agent_state_machine.go`); per-state executors live in `internal/agent/states/`. `internal/agent/tools/registry.go` is the source of truth for registered tools.
- **Bounded contexts (DDD)**, each owning its contracts in a pure `domain/` subpackage that imports nothing internal: `agent`, `conversation`, `scheduler`, `browser`, `computer`. Capabilities without one (`audio`, `mcp`, `skills`, `github`, `channels`, `plugins`) plug into the agent via `agentdomain` service ports. `platform/` is shared infrastructure; `presentation/` is the only place bubbletea, go-telegram and styling appear.
- **Import direction is enforced by depguard** (`.golangci.yml`), not convention: nothing outside `presentation/` may import it or bubbletea; Playwright stays in `browser/`, robotgo in `computer/`, go-telegram in `presentation/telegram/`. `internal/container` and `cmd/` compose everything.

## Import Style

- Import blocks have **six groups** (stdlib / external test libs / testing mocks / external / inference-gateway libs / project), one blank line apart.
- **Every non-stdlib import carries an explicit alias** (enforced by `task lint:imports` + gci). Canonical aliases: `agentdomain`, `convdomain`, `scheddomain`, `browserdomain`, `computerdomain`, `agentinfra`, `agentapp`, `containerruntime`, `githubissues`, `githubsetup`, `adk`, `mockgateway`, `tea` (bubbletea v2), `tests/mocks/<x>` → `<x>mocks`.

## Testing

- Stdlib `testing`, colocated `_test.go`, prefer table-driven. Mocks are **counterfeiter** output committed to `tests/mocks/` — never hand-edit; a new interface in a `domain/` package needs a line in Taskfile's `mocks:generate`.
- Manual runs: never hand-start gateway containers — `flox activate -- go run ./cmd/infer chat` (or `headless <prompt>`) auto-starts the local gateway and tears it down. `INFER_GATEWAY_MOCK=true` exercises the TUI/e2e without a real LLM; the mock matches prompts against scenarios (`INFER_GATEWAY_MOCK_SCENARIOS` overrides).

## Style & Commits

- Linter caps: gocyclo/cyclop 25, funlen 150 lines/80 statements, gocognit 45. Prefer `//nolint:funlen,gocyclo` over splitting cohesive functions.
- Conventional Commits (`.commitlintrc.json`): `feat:`, `fix:`, `docs:`, `chore:`, … `.editorconfig`: two-space indent (tabs in Go), UTF-8, LF, final newline.

## Security Gotchas

- **Bash allow-list is default-deny**, per agent mode (`tools.bash.mode.{all,plan,standard,auto}.allow`; effective list = `mode.all.allow` ∪ the mode's own). Only `auto` is unrestricted; standard/plan are read-only.
- Tool approval is two-layer: `tools.safety.require_approval` (whether) + `approval_behaviour` `prompt|ipc|block` (how). Headless blocks when no approver is reachable.
- Never commit secrets; credentials live in `.env` (never committed).
- `infer init --overwrite` wipes `.infer/agents.yaml` (and `mcp.yaml`, `channels.yaml`, `computer_use.yaml`, `heartbeat.yaml`) — restore with `git checkout -- .infer/agents.yaml` afterwards.

## Config

- Split YAML under `.infer/` (project) and `~/.infer/` (user): `config.yaml`, `prompts.yaml`, `agents.yaml`, `keybindings.yaml`, `mcp.yaml`, `shortcuts/*.yaml`, … Env overrides use `INFER_<PATH_WITH_UNDERSCORES>` (e.g. `INFER_AGENT_MODEL`).
