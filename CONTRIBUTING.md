# Contributing Guide

## Commit Message Convention

This project uses [Conventional Commits](https://www.conventionalcommits.org/) for commit messages. This allows us to automatically generate changelogs and determine version bumps.

### Format

```
<type>[optional scope]: <description>

[optional body]

[optional footer(s)]
```

### Types

- **feat**: A new feature
- **fix**: A bug fix
- **docs**: Documentation only changes
- **style**: Changes that do not affect the meaning of the code (white-space, formatting, missing semi-colons, etc)
- **refactor**: A code change that neither fixes a bug nor adds a feature
- **perf**: A code change that improves performance
- **test**: Adding missing tests or correcting existing tests
- **build**: Changes that affect the build system or external dependencies
- **ci**: Changes to our CI configuration files and scripts
- **chore**: Other changes that don't modify src or test files
- **revert**: Reverts a previous commit

### Examples

- `feat: add chat command for interactive LLM sessions`
- `fix: resolve memory leak in tool execution`
- `docs: update README with installation instructions`
- `feat!: change default config file location` (breaking change)
- `fix(cli): handle missing config file gracefully`

### Breaking Changes

Breaking changes should be indicated by:
1. `!` after the type/scope: `feat!: change API interface`
2. Or a footer: `BREAKING CHANGE: API interface has changed`

## Development Workflow

1. Ensure you have flox installed and activated: `flox activate`
2. Set up development environment: `flox activate -- task setup`
3. Make your changes following the code style guidelines in CLAUDE.md
4. Run tests: `flox activate -- task test`
5. Run quality checks: `flox activate -- task check`
6. Commit with conventional commit messages (pre-commit hooks will run automatically)
7. Push to your fork and create a pull request

## Code Quality Tools

### Pre-commit Hooks

This project uses pre-commit hooks to ensure code quality and consistent formatting:

- **Automatic setup**: Pre-commit hooks are installed when running `task setup`
- **Manual setup**: `flox activate -- task precommit:install`
- **Run on all files**: `flox activate -- task precommit:run`

The hooks automatically:
- Add missing final newlines to files
- Remove trailing whitespace
- Validate YAML/JSON/TOML syntax
- Run golangci-lint on Go code
- Check for merge conflicts

### Available Tasks

```bash
# Development setup
flox activate -- task setup          # Install dependencies and pre-commit hooks

# Code quality
flox activate -- task fmt            # Format Go code
flox activate -- task lint           # Run golangci-lint
flox activate -- task vet            # Run go vet
flox activate -- task check          # Run all quality checks
flox activate -- task precommit:run  # Run pre-commit on all files

# Testing
flox activate -- task test           # Run tests
flox activate -- task test:verbose   # Run tests with verbose output
flox activate -- task test:coverage  # Run tests with coverage

# Building
flox activate -- task build          # Build binary
flox activate -- task release:build  # Build for all platforms
```

## Release Process

Releases are automated using semantic-release:
- Commits to `main` branch trigger automatic releases
- Version numbers are determined by commit types:
  - `fix:` → patch version (1.0.1)
  - `feat:` → minor version (1.1.0)
  - `feat!:` or `BREAKING CHANGE:` → major version (2.0.0)
- Binaries are built for macOS (Intel/ARM64) and Linux (AMD64/ARM64)
- GitHub releases are created automatically with changelogs
