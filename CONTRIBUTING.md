# Contributing Guide

## Commit Message Convention

This project uses [Conventional Commits](https://www.conventionalcommits.org/) for commit messages.
This allows us to automatically generate changelogs and determine version bumps.

### Format

```text
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
2. Install the git pre-commit hook: `flox activate -- task precommit:install`
3. Make your changes following the code style guidelines in AGENTS.md
4. Run tests: `flox activate -- task test`
5. Run quality checks: `flox activate -- task precommit:run`
6. Commit with conventional commit messages (the pre-commit hook runs automatically)
7. Push to your fork and create a pull request

## Code Quality Tools

### Pre-commit Hook

This project uses a plain git hook (`.githooks/pre-commit`, wired via `git config core.hooksPath .githooks` — no pip pre-commit framework):

- **Setup**: `flox activate -- task precommit:install`
- **Run manually**: `flox activate -- task precommit:run`

The hook automatically:

- Rejects trailing whitespace and merge-conflict markers (`git diff --cached --check`)
- Runs `go mod tidy`
- Regenerates mocks when a `*/domain` package (or another counterfeiter source) is staged
- Formats code and runs golangci-lint + markdownlint

### Available Tasks

```bash
# Development setup
flox activate -- task precommit:install  # Install the git pre-commit hook
flox activate -- task mod:download       # Download Go modules

# Code quality
flox activate -- task fmt            # Format Go code
flox activate -- task lint           # Run golangci-lint
flox activate -- task vet            # Run go vet
flox activate -- task precommit:run  # Run all quality checks (the pre-commit hook)

# Testing
flox activate -- task test           # Run tests
flox activate -- task test:verbose   # Run tests with verbose output
flox activate -- task test:coverage  # Run tests with coverage

# Building
flox activate -- task build          # Build binary for the current platform
flox activate -- task release:build  # Build a release binary for the current platform only
flox activate -- task release:build:linux  # Cross-build for Linux (darwin and windows too)
```

## Adding New Tools

Each bounded context owns its tools: agent tools live in `internal/tools/`, browser tools in
`internal/browser/`, computer-use tools in `internal/computer/`. A tool is two things side by side:

- a **manifest** (`<ToolName>.yaml`) - the name, the description and parameter schema the LLM sees, and
  its policy;
- a **Go implementation** - validation and execution.

### Step-by-Step Guide

#### 1. Write the Manifest

Create the manifest next to the context that owns the tool: `internal/tools/YourTool.yaml` for agent
tools, `internal/protocols/a2a/tools/` for the A2A tools, and the package `tools/` directory for browser
and computer tools. The file name must match `name`:

```yaml
name: YourTool
description: Description of what your tool does.
parameters:
  type: object
  properties:
    param1:
      type: string
      description: Description of parameter 1
  required:
    - param1
modes: [standard, auto, auto-with-judge, plan, readonly]  # omit for standard, auto, auto-with-judge
require_approval: false  # omit to inherit tools.safety.require_approval
```

List `readonly` only for a tool that changes nothing: read-only subagents get it, and its calls run
concurrently with other read-only calls. Every mode still advertises the same tools; a call outside the
tool's modes is rejected when it runs.

Add the name to the constants in `tool_manifests.go` (`ToolYourTool = "YourTool"`) and use the constant
everywhere instead of the string. Values only known at runtime (config-driven enums or limits) stay out
of the YAML; set them in `Definition()` with `agentdomain.PropertySchema`.

#### 2. Implement the Tool Interface

Create `internal/tools/your_tool.go`. `Manifest()` hands the registry the tool's policy, and
`Definition()` is built from it:

```go
// Manifest returns the tool's manifest with its configured require_approval.
func (t *YourTool) Manifest() agentdomain.ToolManifest {
    return toolManifests.MustGet(ToolYourTool).WithRequireApproval(t.config.Tools.YourTool.RequireApproval)
}

// Definition returns the tool definition for the LLM.
func (t *YourTool) Definition() sdk.ChatCompletionTool {
    return t.Manifest().Definition()
}

// Execute runs the tool with the given arguments.
func (t *YourTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
    param1, _ := args["param1"].(string)
    return &agentdomain.ToolExecutionResult{ToolName: ToolYourTool, Arguments: args, Success: true, Data: param1}, nil
}

// Validate checks if the tool arguments are valid.
func (t *YourTool) Validate(args map[string]any) error {
    if _, ok := args["param1"].(string); !ok {
        return fmt.Errorf("param1 must be a string")
    }
    return nil
}
```

Drop `WithRequireApproval` when the tool has no `require_approval` setting of its own. A tool whose
approval depends on its arguments implements `agentdomain.CallApprover`, as the computer-use tools do.

#### 3. Register Your Tool

Agent tools register in `internal/tools/registry.go`, keyed by their manifest name:

```go
if cfg.YourService.Enabled {
    r.register(NewYourTool(cfg, r.yourService))
}
```

Tools from another context are built by that context (e.g. `computer.NewTools`) and handed to the
registry by the container via `RegisterTools`.

Then update `internal/loop/testdata/tool_definitions.golden.json` with
`go test ./internal/loop -run TestToolDefinitionsGolden -update` and review the diff.

#### 4. Add Configuration (if needed)

If your tool needs configuration, add it to `config/config.go`:

```go
// Add to the Config struct
type Config struct {
    // ... existing fields
    YourService YourServiceConfig `yaml:"your_service"`
}

type YourServiceConfig struct {
    Enabled bool   `yaml:"enabled"`
    APIKey  string `yaml:"api_key"`
    // Add other config fields
}
```

#### 5. Write Tests

Create `internal/tools/your_tool_test.go`:

```go
package tools

import (
    "context"
    "testing"

    "github.com/inference-gateway/cli/config"
    "github.com/stretchr/testify/assert"
)

func TestYourTool_Definition(t *testing.T) {
    cfg := &config.Config{
        Tools: config.ToolsConfig{Enabled: true},
    }

    tool := NewYourTool(cfg)
    def := tool.Definition()

    assert.Equal(t, "YourTool", def.Function.Name)
    assert.Contains(t, *def.Function.Description, "your tool")
}

func TestYourTool_Execute(t *testing.T) {
    cfg := &config.Config{
        Tools: config.ToolsConfig{Enabled: true},
    }

    tool := NewYourTool(cfg)

    args := map[string]any{
        "param1": "test value",
    }

    result, err := tool.Execute(context.Background(), args)
    assert.NoError(t, err)
    assert.NotNil(t, result)
    assert.Contains(t, result.Output, "test value")
}

func TestYourTool_Validate(t *testing.T) {
    cfg := &config.Config{
        Tools: config.ToolsConfig{Enabled: true},
    }

    tool := NewYourTool(cfg)

    // Test valid args
    validArgs := map[string]any{"param1": "value"}
    assert.NoError(t, tool.Validate(validArgs))

    // Test invalid args
    invalidArgs := map[string]any{}
    assert.Error(t, tool.Validate(invalidArgs))
}
```

#### 6. Test Your Implementation

Run the test suite to ensure your tool works correctly:

```bash
# Run all tests
flox activate -- task test

# Run tests for your specific tool
flox activate -- go test ./internal/tools -run TestYourTool

# Run with verbose output
flox activate -- task test:verbose
```

#### 7. Update Documentation

Consider adding usage examples to the main README.md if your tool adds significant functionality.

### Best Practices

1. **Security**: Always validate input parameters and implement proper error handling
2. **Configuration**: Make tools configurable and respect the global `tools.enabled` setting
3. **Error Handling**: Return meaningful error messages that help users understand what went wrong
4. **Testing**: Write comprehensive tests including edge cases and error conditions
5. **Documentation**: Use clear, descriptive names and comprehensive parameter descriptions
6. **Dependencies**: Minimize external dependencies and use dependency injection for services
7. **Context**: Always respect the context for cancellation and timeouts

### Example Tools

Study the existing tools for implementation patterns:

- **BashTool** (`bash.go`): Shows command execution with security validation
- **ReadTool** (`read.go`): Demonstrates file system operations
- **GrepTool** (`grep.go`): Shows complex parameter handling with ripgrep integration
- **WebSearchTool** (`web_search.go`): Shows integration with external services and runtime schema values

## Release Process

Releases are cut by dispatching the release workflow by hand. semantic-release runs inside that
workflow:

- Commits to `main` branch do not release on their own - dispatch the workflow to cut one
- Version numbers are determined by commit types:
  - `fix:` → patch version (1.0.1)
  - `feat:` → minor version (1.1.0)
  - `feat!:` or `BREAKING CHANGE:` → major version (2.0.0)
- Binaries are built for macOS (Intel/ARM64), Linux (AMD64/ARM64) and Windows (AMD64/ARM64)
- GitHub releases are created automatically with changelogs
