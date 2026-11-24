# AGENTS.md

A README for coding agents on the **Inference Gateway CLI** — an agentic CLI (chat TUI, headless agent, A2A agents, tool execution). User-facing docs live in README.md and docs/. Contributors start at CONTRIBUTING.md.

## Stack

- Entry: `cmd/infer/main.go` → `root.Execute()` (Cobra). `internal/container/container.go` is the DI composition root.
- Dev env pinned by **flox** (`.flox/env/manifest.toml`). Run everything through `flox activate --`.

## Build / Test / Lint

`task --list` shows the build, test, lint and mock tasks. **Run `task precommit:run` before every push** — it's the CI gate, and it aborts if `task fmt` reformats your staged files (re-`git add` and retry).

## Architecture

The Go code is **bounded contexts (DDD)** under `internal/`. `internal/AGENTS.md` maps the contexts and holds the security gotchas (Bash allow-list, sandbox path policy, tool approval). Read it before changing code there.

Repo-wide invariants:

- Import direction is enforced by depguard (`.golangci.yml`), not convention: nothing outside `presentation/` may import it or bubbletea, except the `internal/container` composition root. The A2A ADK stays in `protocols/a2a/`, the AG-UI SDK and the binding's socket in `protocols/agui/`, whose import list is closed (see its README), Playwright in `browser/`, robotgo in `computer/`, go-telegram in `presentation/telegram/`, and the tools context never imports the loop (`agent/loop`) back. `domain/` packages stay pure (the depguard rule covers the `<context>/domain/` packages and the nested `protocols/*/domain` ones), and only `cmd/` may import `internal/container`.
- `internal/tools/registry.go` is the source of truth for registered tools. Read `internal/tools/AGENTS.md` before touching a tool manifest, a tool name or the registry.

## Package AGENTS files

`internal/AGENTS.md`, `internal/tools/AGENTS.md` and `internal/presentation/AGENTS.md` hold directory-scoped rules and take precedence for their directory. `infer` injects the `AGENTS.md` in its working directory, capped at 8000 chars, and only points at the nested ones, so read the nested one before editing there.

## Import Style

- **Every non-stdlib import carries an explicit alias** (enforced by `task lint:imports` + gci). Canonical aliases: `agentdomain`, `agentloop`, `convdomain`, `scheddomain`, `a2adomain`, `browserdomain`, `computedomain`, `computerdomain`, `mcpdomain`, `agentinfra`, `a2ainfra`, `mcpinfra`, `schedinfra`, `sandbox`, `sandboxdomain`, `sandboxinfra`, `agui`, `containerruntime`, `githubissues`, `githubsetup`, `agentheadless`, `agentteleport`, `k8sworkspace`, `k8sdynamic`, `k8sclientcmd`, `operatorv1alpha1`, `tools`, `customtools`, `adk`, `mockgateway`, `tea` (bubbletea v2), `tests/mocks/<x>` → `<x>mocks`.

## Testing

- Stdlib `testing`, colocated `_test.go`, prefer table-driven. Never hand-write fake or mock structs in `_test.go`, not even for a single method. Every test double is **counterfeiter** output committed to `tests/mocks/`, never hand-edited. For an interface without a fake, add a line to Taskfile's `mocks:generate`, run `task mocks:generate`, and configure the fake's `Stub`/`Returns` fields from the test.
- Manual runs: never hand-start gateway containers — `flox activate -- go run ./cmd/infer chat` (or `headless <prompt>`) auto-starts the local gateway and tears it down. `INFER_GATEWAY_MOCK=true` exercises the TUI/e2e without a real LLM. The mock matches prompts against scenarios (`INFER_GATEWAY_MOCK_SCENARIOS` overrides).

## Style & Commits

- Prefer `//nolint:funlen,gocyclo` over splitting cohesive functions (limits in `.golangci.yml`).
- Write self-explanatory code: clear names and small, single-purpose functions carry the intent.
  If a block needs a comment to be understood, extract it into a well-named function or variable.
- No inline comments inside function bodies.
- Doc comments on functions and types are at most 5 lines: what it does and why, not how.
  Aim for 3. Genuinely multi-step docs (ordered lists, state routing) restructure their steps into the code body instead of growing the docblock. Never reference GitHub issues or PRs - ticket context belongs in commit messages, PR bodies, and CHANGELOG.md.
- No semicolons in documentation prose (Markdown files, doc comments): split the sentence or use
  a dash instead. The same goes for commit messages and PR bodies.
- No comments above modules, packages, or files: no Go package doc comment above the `package`
  line, no Rust `//!` crate or module docs, no Python module docstring, no TypeScript `@fileoverview`
  or `@module` block, no file header.
- Document a struct, class, or record once above its declaration, not on each member. If a field
  needs explaining, rename it or fold the explanation into the type's doc comment.
- Prefer named constants or enums over magic numbers and strings. A bare literal is fine only
  where its meaning is obvious at a glance (0, 1, an empty string).
- Examples (`examples/` directories, README snippets) are short and focused: one feature per
  example, the fewest lines that show it, and the exact command to run it.
- Every example must run as written. Verify it against the `tokenless` mock gateway, or against a
  real model such as `ollama_cloud/glm-5.3-flash` when the mock cannot cover it. Fix or delete an
  example that does not run.
- Tool directives are not comments and stay where the tool needs them (lint suppressions, build
  tags, compiler pragmas, code generation markers). Here: `//nolint:...`, `//go:...` (incl. `//go:build`, `//go:generate`), `#nosec`.
- Conventional Commits (`.commitlintrc.json`): `feat:`, `fix:`, `docs:`, `chore:`, …
- YAML uses block style: one `- item` per line for sequences and one `key: value` per line for mappings. Never
  flow style (`[a, b]`, `{ key: value }`), in manifests, scenarios and config alike.

## Config

- Split YAML under `.infer/` (project) and `~/.infer/` (user): `config.yaml`, `sandbox.yaml`, `prompts.yaml`, `agents.yaml`, `keybindings.yaml`, `mcp.yaml`, `shortcuts/*.yaml`, … Env overrides use `INFER_<PATH_WITH_UNDERSCORES>` (e.g. `INFER_AGENT_MODEL`).
- Never commit secrets. Credentials live in `.env` (never committed).
- `infer init --overwrite` regenerates every seeded userspace file (`config.yaml`, `agents.yaml`, `mcp.yaml`, `prompts.yaml`, `hooks.yaml`, `keybindings.yaml`, `channels.yaml`, `sandbox.yaml`, every `shortcuts/*.yaml`, and the rest of the seed list in `cmd/init/init.go`). Only `memory.yaml` survives - it is deliberately never clobbered by a re-init. `browser_use.yaml` is regenerated but keeps its `extension.token`, which the opentask extension and a running daemon authenticate with. None of them is ever in the repository.
