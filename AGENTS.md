# AGENTS.md

A README for coding agents on the **Inference Gateway CLI** — an agentic CLI (chat TUI, headless agent, A2A agents, tool execution). User-facing docs live in README.md and docs/. Contributors start at CONTRIBUTING.md.

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

The codebase is **bounded contexts (DDD)** under `internal/`. Each context owns its contracts in a `domain/` subpackage that imports nothing internal except `agent/domain`, the shared kernel (tool results, agent mode, chat events). Tool contracts stay in the shared kernel because the a2a, browser, computer and MCP tools implement them. Adapters sit in `<context>/infrastructure/`, the protocol integrations (`a2a`, `mcp`, `agui`) under `internal/protocols/`, and `platform/` is shared infrastructure. `protocols/agui` is general purpose: a writer for the events of one run and a WebSocket binding with both of its ends. It names no client and no context, and its only internal import is the logger. Everything else imports it: `presentation/headless` maps agent chat events onto a run and owns the panel units the serve worker answers with, `browser` owns the extension relay and client on top of the binding, and `computer` returns the CUSTOM events it publishes. `cmd/daemon` hosts the binding and puts the browser relay and `sessions`, the thread registry that supervises one `headless --serve` worker per thread, behind it.

Contexts: `agent`, `binaries`, `browser`, `computer`, `conversation`, `scheduler`, `sessions`, `tools`, `protocols/{a2a,mcp,agui}`. Capabilities, which have no `domain/`: `audio`, `channels`, `github`, `plugins`, `skills`. `avatars`, `daemon`, `gateway`, `insights` and `provisioner` are single-package support code.

**Before changing a context or capability, read its `internal/<name>/README.md`.** It covers what it is, why it exists, and how it plugs in.

Repo-wide invariants:

- Import direction is enforced by depguard (`.golangci.yml`), not convention: nothing outside `presentation/` may import it or bubbletea; the A2A ADK stays in `protocols/a2a/`, the AG-UI SDK and the binding's socket in `protocols/agui/`, which imports nothing internal but the logger, Playwright in `browser/`, robotgo in `computer/`, go-telegram in `presentation/telegram/`, and the tools context never imports the agent context back (the `agent/domain` shared kernel excepted). `domain/` packages stay pure, and only `cmd/` may import `internal/container`.
- `internal/tools/registry.go` is the source of truth for registered tools. Read `internal/tools/AGENTS.md` before touching a tool manifest, a tool name or the registry.

## Package AGENTS files

`internal/tools/AGENTS.md` and `internal/presentation/AGENTS.md` hold package-only rules and take precedence for their directory. `infer` injects the `AGENTS.md` in its working directory and only points at the nested ones, so read the nested one before editing there.

## Import Style

- Import blocks have **six groups** (stdlib / external test libs / testing mocks / external / inference-gateway libs / project), one blank line apart.
- **Every non-stdlib import carries an explicit alias** (enforced by `task lint:imports` + gci). Canonical aliases: `agentdomain`, `convdomain`, `scheddomain`, `a2adomain`, `browserdomain`, `computerdomain`, `mcpdomain`, `agentinfra`, `a2ainfra`, `mcpinfra`, `schedinfra`, `agui`, `containerruntime`, `githubissues`, `githubsetup`, `tools`, `customtools`, `adk`, `mockgateway`, `tea` (bubbletea v2), `tests/mocks/<x>` → `<x>mocks`.

## Testing

- Stdlib `testing`, colocated `_test.go`, prefer table-driven. Mocks are **counterfeiter** output committed to `tests/mocks/` — never hand-edit; a new interface in a `domain/` package needs a line in Taskfile's `mocks:generate`.
- Manual runs: never hand-start gateway containers — `flox activate -- go run ./cmd/infer chat` (or `headless <prompt>`) auto-starts the local gateway and tears it down. `INFER_GATEWAY_MOCK=true` exercises the TUI/e2e without a real LLM; the mock matches prompts against scenarios (`INFER_GATEWAY_MOCK_SCENARIOS` overrides).

## Style & Commits

- Linter caps: gocyclo/cyclop 25, funlen 150 lines/80 statements, gocognit 45. Prefer `//nolint:funlen,gocyclo` over splitting cohesive functions.
- Write self-explanatory code: clear names and small, single-purpose functions carry the intent.
  If a block needs a comment to be understood, extract it into a well-named function or variable.
- No inline comments inside function bodies.
- Doc comments on functions and types are at most 5 lines: what it does and why, not how.
  Aim for 3. Genuinely multi-step docs (ordered lists, state routing) restructure their steps into the code body instead of growing the docblock. Never reference GitHub issues or PRs - ticket context belongs in commit messages, PR bodies, and CHANGELOG.md.
- No semicolons in doc comments, commit messages or PR bodies. Split the clauses into separate sentences.
- No comments above modules, packages, or files.
- Tool directives are not comments and stay where the tool needs them (lint suppressions, build
  tags, compiler pragmas, code generation markers). Here: `//nolint:...`, `//go:...` (incl. `//go:build`, `//go:generate`), `#nosec`.
- Conventional Commits (`.commitlintrc.json`): `feat:`, `fix:`, `docs:`, `chore:`, … `.editorconfig`: two-space indent (tabs in Go), UTF-8, LF, final newline.

## Security Gotchas

- **Bash allow-list is default-deny**, per agent mode (`tools.bash.mode.{all,plan,standard,auto}.allow`; effective list = `mode.all.allow` ∪ the mode's own). Only `auto` is unrestricted; standard/plan are read-only, and an allowed command still asks when a path it names leaves the sandbox (`config/bash_paths.go`). `auto-with-judge` maps to the `standard` bucket — the judge gates calls, it never widens the list.
- Tool approval is two-layer: `tools.safety.require_approval` (whether) + `approval_behaviour` `prompt|ipc|judge|block` (how). `judge` routes gated calls to an LLM judge (config `judge.yaml`; forced by the `auto-with-judge` agent mode — see docs/judge-mode.md). Headless blocks when no approver is reachable.
- Project custom tools (`.infer/tools/`, `.agents/tools/`) always need approval outside auto mode, whatever their manifest says.
- Never commit secrets; credentials live in `.env` (never committed).
- `infer init --overwrite` wipes `.infer/agents.yaml` (and `mcp.yaml`, `channels.yaml`, `computer_use.yaml`, `heartbeat.yaml`, `judge.yaml`) — restore with `git checkout -- .infer/agents.yaml` afterwards.

## Config

- Split YAML under `.infer/` (project) and `~/.infer/` (user): `config.yaml`, `prompts.yaml`, `agents.yaml`, `keybindings.yaml`, `mcp.yaml`, `shortcuts/*.yaml`, … Env overrides use `INFER_<PATH_WITH_UNDERSCORES>` (e.g. `INFER_AGENT_MODEL`).
