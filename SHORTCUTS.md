# Extensible Shortcuts System

The CLI provides an extensible shortcuts system that allows you to quickly execute common commands with
`/shortcut-name` syntax.

## Built-in Shortcuts

### Core Shortcuts

- `/clear` - Clear conversation history
- `/exit` - Exit the chat session
- `/help [shortcut]` - Show available shortcuts or specific shortcut help
- `/switch [model]` - Switch to a different model
- `/config <show|get|set|reload> [key] [value]` - Manage configuration settings
- `/compact [format]` - Export conversation to markdown

### Git Shortcuts

- `/git <command> [args...]` - Execute git commands (supports commit, push, status, etc.)
- `/git commit [flags]` - **NEW**: Commit staged changes with AI-generated message
- `/git push [remote] [branch] [flags]` - **NEW**: Push commits to remote repository

The git shortcuts provide intelligent commit message generation using AI when no message is provided with `/git commit`.

## User-Defined Shortcuts

You can create custom shortcuts by adding YAML configuration files in the `.infer/shortcuts/` directory.

### Configuration File Format

Create files named `custom-*.yaml` (e.g., `custom-1.yaml`, `custom-dev.yaml`) in `.infer/shortcuts/`:

```yaml
shortcuts:
  - name: "tests"
    description: "Run all tests in the project"
    command: "go"
    args: ["test", "./..."]
    working_dir: "."  # Optional: set working directory

  - name: "build"
    description: "Build the project"
    command: "go"
    args: ["build", "-o", "infer", "."]

  - name: "lint"
    description: "Run linter on the codebase"
    command: "golangci-lint"
    args: ["run"]
```

### Configuration Fields

- **name** (required): The shortcut name (used as `/name`)
- **description** (required): Human-readable description shown in `/help`
- **command** (required): The executable command to run
- **args** (optional): Array of arguments to pass to the command
- **working_dir** (optional): Working directory for the command (defaults to current)

### Usage Examples

With the configuration above, you can use:

- `/tests` - Runs `go test ./...`
- `/build` - Runs `go build -o infer .`
- `/lint` - Runs `golangci-lint run`

You can also pass additional arguments:

- `/tests -v` - Runs `go test ./... -v`
- `/build --race` - Runs `go build -o infer . --race`

## Example Custom Shortcuts

Here are some useful shortcuts you might want to add:

### Development Shortcuts (`custom-dev.yaml`)

```yaml
shortcuts:
  - name: "fmt"
    description: "Format all Go code"
    command: "go"
    args: ["fmt", "./..."]

  - name: "mod tidy"
    description: "Tidy up go modules"
    command: "go"
    args: ["mod", "tidy"]

  - name: "version"
    description: "Show current version"
    command: "git"
    args: ["describe", "--tags", "--always", "--dirty"]
```

### Docker Shortcuts (`custom-docker.yaml`)

```yaml
shortcuts:
  - name: "docker build"
    description: "Build Docker image"
    command: "docker"
    args: ["build", "-t", "myapp", "."]

  - name: "docker run"
    description: "Run Docker container"
    command: "docker"
    args: ["run", "-p", "8080:8080", "myapp"]
```

### Project-Specific Shortcuts (`custom-project.yaml`)

```yaml
shortcuts:
  - name: "migrate"
    description: "Run database migrations"
    command: "./scripts/migrate.sh"
    working_dir: "."

  - name: "seed"
    description: "Seed database with test data"
    command: "go"
    args: ["run", "cmd/seed/main.go"]
```

## Tips

1. **File Organization**: Use descriptive names for your config files (e.g., `custom-dev.yaml`, `custom-docker.yaml`)
2. **Command Discovery**: Use `/help` to see all available shortcuts including your custom ones
3. **Error Handling**: If a custom shortcut fails to load, it will be skipped with a warning
4. **Reloading**: Restart the chat session to reload custom shortcuts after making changes
5. **Security**: Be careful with custom shortcuts as they execute system commands

## Troubleshooting

- **Shortcut not appearing**: Check YAML syntax and file naming (`custom-*.yaml`)
- **Command not found**: Ensure the command is available in your PATH
- **Permission denied**: Check file permissions and executable rights
- **Invalid YAML**: Use a YAML validator to check your configuration syntax
