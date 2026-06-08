# Repository Guidelines

## Project Structure & Module Organization

This repository is a Go CLI application for Inference Gateway. The entry point is `main.go`; Cobra command implementations live in `cmd/`. Configuration models, defaults, and YAML helpers live in `config/`. Shared implementation details are under `internal/`, with test doubles in `tests/mocks/`. User-facing documentation is in `docs/`, runnable scenarios are in `examples/`, and release/container files include `Taskfile.yml`, `Dockerfile`, `flake.nix`, and `.releaserc.yaml`.

## Build, Test, and Development Commands

Use `flox activate` when available so local tools match CI.

- `task build`: builds the `infer` binary with version metadata.
- `task run -- <args>`: runs the CLI locally, for example `task run -- status`.
- `task test`: runs all Go tests with `go test ./...`.
- `task test:coverage`: runs tests with package coverage output.
- `task fmt`: formats Go files with `go fmt ./...`.
- `task lint`: runs `golangci-lint` and markdownlint fixes.
- `task vet`: runs `go vet ./...`.
- `task precommit:install` / `task precommit:run`: installs or runs all pre-commit checks.

## Coding Style & Naming Conventions

Follow `.editorconfig`: UTF-8, LF endings, final newline, two-space indentation by default, and tabs for Go files. Keep Go code idiomatic and formatted by `go fmt`. Package names are short, lowercase, and descriptive. Go tests should use `_test.go` files colocated with the package unless shared mocks belong in `tests/mocks/`. Keep command names and config keys consistent with existing CLI terminology.

## Testing Guidelines

The project uses Go’s standard testing framework. Add or update focused tests for command behavior, config parsing, migrations, and service logic touched by a change. Prefer table-driven tests where inputs and expected results vary. Run `task test` before opening a PR; use `task test:verbose` while diagnosing failures and `task test:coverage` for broader changes.

## Commit & Pull Request Guidelines

Use Conventional Commits as enforced by `.commitlintrc.json`: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`, or `revert`. Examples: `fix(cli): handle missing config file` or `docs: update mcp guide`. PRs should describe the change, include test results, link related issues, and include screenshots or terminal output when user-visible CLI behavior changes.

## After Editing Config Behaviour

When you change defaults, struct fields, validation, or any other behaviour in `config/config.go` (or any file under `config/` that affects generated output):

1. **Re-generate the config files** by running `go run . init --overwrite`. This ensures the on-disk YAML files reflect the new defaults and schema.
2. **Discard the `agents.yaml` diff.** `go run . init --overwrite` also regenerates `agents.yaml` — but that file contains user-curated A2A agent registrations. Overwriting it would nuke any agents the user has configured, so always `git checkout -- .infer/agents.yaml` (or restore it) before committing. The same applies to `mcp.yaml`, `channels.yaml`, `computer_use.yaml`, and `heartbeat.yaml` if they contain user data — use your judgement based on what you changed.

## Security & Configuration Tips

Do not commit real secrets. Start from `.env.example` files and keep local credentials in `.env`. When changing tool execution, filesystem, MCP, or provider configuration behavior, review related docs under `docs/` and add tests for restrictive or failure cases.
