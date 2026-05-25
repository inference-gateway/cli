# Repository Guidelines

## Project Structure & Module Organization

This repository contains the Go source for the `infer` CLI. The entry point is `main.go`, with Cobra commands and command tests in `cmd/`. Shared configuration types live in `config/`, implementation details live in `internal/`, and reusable test fakes are under `tests/mocks/`. User-facing docs belong in `docs/`; runnable compose examples are grouped in `examples/`. Build outputs are written to `dist/` or the root `infer` binary.

## Build, Test, and Development Commands

Use `task` for local workflows, preferably inside Flox.

- `flox activate -- task build`: builds the local `infer` binary.
- `flox activate -- task run -- <args>`: runs the CLI via `go run`, passing CLI arguments.
- `flox activate -- task test`: runs `go test ./...`.
- `flox activate -- task test:coverage`: runs tests with package coverage.
- `flox activate -- task fmt`: formats Go files with `go fmt ./...`.
- `flox activate -- task lint`: runs `golangci-lint` and markdownlint.
- `flox activate -- task precommit:run`: runs all configured pre-commit checks.

## Coding Style & Naming Conventions

Follow `.editorconfig`: UTF-8, LF endings, final newline, two-space indentation by default, and tabs for Go files. Keep Go package names short, lowercase, and consistent with their directory. Test files use `*_test.go`; mocks in `tests/mocks/` are named by dependency, for example `fake_agent_service.go`. Run `task fmt` before submitting code.

## Testing Guidelines

Tests use Go's standard testing package. Place unit tests next to the code they exercise, as in `cmd/root_test.go` or `config/config_test.go`. Use `tests/mocks/` for shared fakes. Add or update tests for command behavior, config defaults, migrations, and error handling. Run `task test` for normal validation and `task test:coverage` when changing shared packages.

## Commit & Pull Request Guidelines

Commits must follow Conventional Commits enforced by `.commitlintrc.json`, such as `feat: add provider option`, `fix(cli): handle missing config`, or `docs: update install guide`. Allowed types include `feat`, `fix`, `docs`, `refactor`, `test`, `build`, `ci`, and `chore`; use lowercase subjects without trailing periods.

Before opening a pull request, run `task precommit:run` and `task test`. PRs should describe the change, mention related issues, call out config or migration impacts, and include screenshots or terminal output when user-visible CLI behavior changes.

## Security & Configuration Tips

Never commit secrets from `.env`; use `.env.example` for documented placeholders. Review changes to `examples/*/docker-compose.yml`, MCP settings, and tool execution paths carefully because they affect local runtime behavior.
