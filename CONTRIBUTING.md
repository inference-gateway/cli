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

# UI Testing
flox activate -- task test:ui:snapshots     # Generate UI component snapshots
flox activate -- task test:ui:verify        # Verify UI snapshots match current output
flox activate -- task test:ui:interactive   # Show UI component testing examples

# Building
flox activate -- task build          # Build binary
flox activate -- task release:build  # Build for all platforms
```

## Adding New Tools

The CLI uses a modular tools architecture where each tool is implemented as a separate module in the
`internal/services/tools/` package. This section describes how to add new tools for LLM integration.

### Tool Architecture Overview

```text
internal/services/tools/
├── interfaces.go      # Tool interface definitions
├── registry.go        # Tool management and registration
├── bash.go           # Example: Bash command execution tool
├── read.go           # Example: File reading tool
├── grep.go           # Example: Grep tool
├── fetch.go          # Example: Content fetching tool
├── websearch.go      # Example: Web search tool
└── [your-tool].go    # Your new tool implementation
```

### Step-by-Step Guide

#### 1. Create Your Tool File

Create a new file `internal/services/tools/your_tool.go`:

```go
package tools

import (
    "context"
    "fmt"

    "github.com/inference-gateway/cli/config"
    "github.com/inference-gateway/cli/internal/domain"
    "github.com/inference-gateway/sdk"
)

// YourTool handles your specific functionality
type YourTool struct {
    config  *config.Config
    enabled bool
    // Add any additional dependencies here
}

// NewYourTool creates a new instance of your tool
func NewYourTool(cfg *config.Config /* add other dependencies */) *YourTool {
    return &YourTool{
        config:  cfg,
        enabled: cfg.Tools.Enabled, // or specific config section
        // Initialize dependencies
    }
}
```

#### 2. Implement the Tool Interface

Your tool must implement the `Tool` interface defined in `interfaces.go`:

```go
// Definition returns the tool definition for the LLM
func (t *YourTool) Definition() sdk.ChatCompletionTool {
    description := "Description of what your tool does"
    parameters := sdk.FunctionParameters(map[string]any{
        "type": "object",
        "properties": map[string]any{
            "param1": map[string]any{
                "type":        "string",
                "description": "Description of parameter 1",
            },
            "param2": map[string]any{
                "type":        "integer",
                "description": "Description of parameter 2",
                "minimum":     1,
            },
        },
        "required": []string{"param1"},
    })

    return sdk.ChatCompletionTool{
        Type: sdk.Function,
        Function: sdk.FunctionObject{
            Name:        "YourTool",
            Description: &description,
            Parameters:  &parameters,
        },
    }
}

// Execute runs the tool with given arguments
func (t *YourTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
    if !t.enabled {
        return nil, fmt.Errorf("YourTool is not enabled")
    }

    // Validate and extract arguments
    param1, ok := args["param1"].(string)
    if !ok {
        return nil, fmt.Errorf("param1 must be a string")
    }

    // Implement your tool logic here
    result := fmt.Sprintf("Processing: %s", param1)

    return &domain.ToolExecutionResult{
        Output: result,
        // Add other result fields as needed
    }, nil
}

// Validate checks if the tool arguments are valid
func (t *YourTool) Validate(args map[string]any) error {
    // Implement validation logic
    if _, exists := args["param1"]; !exists {
        return fmt.Errorf("param1 is required")
    }
    return nil
}

// IsEnabled returns whether this tool is enabled
func (t *YourTool) IsEnabled() bool {
    return t.enabled
}
```

#### 3. Register Your Tool

Add your tool to the registry in `internal/services/tools/registry.go`:

```go
// In the registerTools() method, add:
r.tools["YourTool"] = NewYourTool(r.config /* add dependencies */)
```

For conditional tools (e.g., requiring external services or configuration):

```go
// Example for conditional registration
if r.config.YourService.Enabled {
    r.tools["YourTool"] = NewYourTool(r.config, r.yourService)
}
```

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

Create `internal/services/tools/your_tool_test.go`:

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
flox activate -- go test ./internal/services/tools -run TestYourTool

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
- **WebSearchTool** (`websearch.go`): Shows integration with external services

## Testing UI Components

The CLI includes a specialized internal `test-view` command for testing and iterating on UI components directly in the
terminal, without needing to enter chat mode. This command is hidden from users but available for developers.

> **Note**: The `test-view` command is internal-only and won't appear in `infer --help`. It's designed for
> development, testing, and snapshot generation purposes.

### Available Components

#### Approval View Component

Test the tool approval UI that appears when tools require user approval:

```bash
# Test approval view with default sample data
flox activate -- ./infer test-view approval

# Test with custom dimensions
flox activate -- ./infer test-view approval --width 120 --height 40
```

This renders the approval component with:

- Sample Edit tool data showing a diff preview
- Different selection states (approve/deny options)
- Proper styling with Tokyo Night theme
- Scrollable content for long tool arguments

#### Diff Renderer Component

Test the colored diff renderer used in Edit and MultiEdit tool previews:

```bash
# Test diff renderer with default sample data
flox activate -- ./infer test-view diff

# Test with custom content
flox activate -- ./infer test-view diff --old "original code" --new "modified code"
```

This renders:

- Edit tool argument formatting with colored diffs
- MultiEdit tool with multiple operations
- Pure diff rendering with context lines
- Proper ANSI color codes for additions/removals

#### MultiEdit Tool Component

Test the MultiEdit tool's collapsed and expanded view formatting:

```bash
# Test MultiEdit tool collapsed view formatting
flox activate -- ./infer test-view multiedit

# Test with snapshot mode for automated testing
flox activate -- ./infer test-view multiedit --snapshot
```

This demonstrates:

- Collapsed view showing file name and edit count
- Success/failure status indicators
- Expanded diff preview with multiple edits
- Large-scale edit operations (10+ edits)
- Clean snapshot output for automated testing

#### Large File Testing Component

Test UI responsiveness with large content and different terminal sizes:

```bash
# Test large file editing at different terminal sizes
flox activate -- ./infer test-view large-file
```

This tests:

- Component rendering at various terminal dimensions
- Performance with large file content
- Scrollable content handling
- Write tool preview for large files

### Customization Options

All components support customization through CLI flags:

- `--width INT`: Set component width (default: 100)
- `--height INT`: Set component height (default: 30)
- `--old TEXT`: Custom old content for diff testing
- `--new TEXT`: Custom new content for diff testing
- `--snapshot`: Output clean, parseable format for automated testing

### Use Cases

- **UI Development**: Rapidly iterate on component styling and layout without entering chat mode
- **Design Testing**: Test components at different terminal sizes and with various content lengths
- **Color Verification**: Ensure proper color rendering across different terminal environments
- **Accessibility**: Test readability and contrast of UI elements
- **Snapshot Testing**: Generate consistent output for automated UI regression tests
- **Performance Testing**: Test component rendering with large content at different terminal sizes

### Automated Snapshot Testing

The project includes automated snapshot testing for UI components using Task commands:

```bash
# Generate baseline snapshots (run after UI changes)
flox activate -- task test:ui:snapshots

# Verify current UI matches snapshots (run in CI/testing)
flox activate -- task test:ui:verify

# Get help with interactive testing
flox activate -- task test:ui:interactive
```

**Snapshot Files**: Stored in `test/snapshots/ui/` with clean, parseable format for version control

**CI Integration**: The `test:ui:verify` task can be integrated into CI pipelines to catch UI regressions

**Workflow**:

1. Make UI changes to components
2. Run `task test:ui:snapshots` to update baselines
3. Commit snapshot files along with code changes
4. CI runs `task test:ui:verify` to ensure no unintended changes

### Adding New Testable Components

To add a new component to the test-view command:

1. **Implement the Component**: Create your component in `internal/ui/components/`
2. **Add Test Case**: Add a new case to `cmd/test_view.go` in the `runTestView` function
3. **Create Test Function**: Implement a `test[ComponentName]` function following existing patterns
4. **Update Help**: Add your component to the command description and help text

## Release Process

Releases are automated using semantic-release:

- Commits to `main` branch trigger automatic releases
- Version numbers are determined by commit types:
  - `fix:` → patch version (1.0.1)
  - `feat:` → minor version (1.1.0)
  - `feat!:` or `BREAKING CHANGE:` → major version (2.0.0)
- Binaries are built for macOS (Intel/ARM64) and Linux (AMD64/ARM64)
- GitHub releases are created automatically with changelogs
