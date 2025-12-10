# Shortcuts Example

This example demonstrates how to use custom shortcuts in the Inference Gateway CLI.

## What are Shortcuts?

Shortcuts are user-defined commands that execute shell commands, scripts,
or AI-powered operations within the chat interface. They support:

- **Simple commands**: Execute any shell command or script
- **AI-powered snippets**: Execute commands, send output to LLM, use response in templates
- **Parameter support**: Pass arguments to shortcuts
- **JSON output parsing**: Structure command output for AI processing

> **Container Environment**: The CLI runs in Alpine Linux with `bash` and `jq` pre-installed for scripting and JSON processing.

## Quick Start

1. **Copy environment file**:

   ```bash
   cp .env.example .env
   ```

2. **Add your API key** to `.env`:

   ```bash
   ANTHROPIC_API_KEY=your-key-here
   # or
   OPENAI_API_KEY=your-key-here
   ```

3. **Start the services**:

   ```bash
   docker compose up -d
   ```

4. **Run the CLI**:

   ```bash
   docker compose run --rm cli
   ```

5. **Try the shortcuts**:

   ```text
   /hello
   /sysinfo
   /review-comments
   ```

## Custom Shortcuts

This example includes three types of shortcuts in `.infer/shortcuts/custom-demo.yaml`:

### 1. Simple Echo Shortcut

```yaml
- name: hello
  description: "Say hello from the container"
  command: echo
  args:
    - "Hello from the Inference Gateway CLI! 🚀"
```

**Usage**: `/hello`

### 2. System Information Shortcut

```yaml
- name: sysinfo
  description: "Display system information"
  command: bash
  args:
    - -c
    - |
      echo "=== System Information ==="
      echo "Hostname: $(hostname)"
      # ... more commands
```

**Usage**: `/sysinfo`

### 3. AI-Powered Review Comment Reply Generator

```yaml
- name: review-comments
  description: "Generate suggested replies to code review comments"
  command: bash
  args:
    - -c
    - |
      # Fetch mock GitHub review comments
      jq -n '{repo: "...", prNumber: "123", comments: [...]}'
  snippet:
    prompt: |
      Draft professional replies to review comments...
      Generate a gh CLI command to post all replies.
    template: |
      ! {llm}
```

**Usage**: `/review-comments`

**How it works**:

1. Fetches mock review comments from a PR (simulates GitHub API)
2. AI analyzes the comments and drafts professional replies
3. AI generates an executable `gh` CLI command with the replies
4. Command is placed in your input box with `!` prefix
5. **Review and press Enter** to execute and post the replies to GitHub

This demonstrates the **snippet + template** feature where:

- The **command** outputs JSON data
- The **snippet** sends data to AI with instructions
- The **template** formats the AI response as an executable command
- Perfect for workflows that need human review before execution!

## Shortcut Structure

### Basic Shortcut

```yaml
shortcuts:
  - name: shortcut-name
    description: "What this shortcut does"
    command: bash
    args:
      - -c
      - "echo 'Hello World'"
```

### AI-Powered Shortcut (with Snippet)

```yaml
shortcuts:
  - name: ai-shortcut
    description: "AI-powered operation"
    command: bash
    args:
      - -c
      - |
        # Execute command and output JSON
        jq -n --arg data "value" '{result: $data}'
    snippet:
      prompt: |
        Analyze this data: {result}

        Provide insights based on the data.
      template: |
        ## Analysis Results

        {llm}
```

**How it works**:

1. `command` executes and produces output (preferably JSON)
2. Output fields become variables (e.g., `{result}`)
3. `prompt` is sent to the LLM with variable substitution
4. LLM response becomes `{llm}` variable
5. `template` formats the final output shown to user

## Built-in Shortcuts

The CLI includes several built-in shortcuts (available by default):

- `/help` - Show help information
- `/clear` - Clear conversation history
- `/exit` - Exit the CLI
- `/switch <conversation-id>` - Switch conversations
- `/theme <theme-name>` - Change UI theme

## Adding Your Own Shortcuts

1. **Create a new file** in `.infer/shortcuts/`:

   ```bash
   touch .infer/shortcuts/custom-myshortcuts.yaml
   ```

2. **Define your shortcuts**:

   ```yaml
   shortcuts:
     - name: my-command
       description: "My custom command"
       command: echo
       args:
         - "My output"
   ```

3. **Restart the CLI** or reload configuration

4. **Use your shortcut**: `/my-command`

## Tips

- **File naming**: Use `custom-*.yaml` for custom shortcuts
- **Shell support**: Both `bash` and `sh` are available in the container
- **JSON output**: Use `jq` to format command output for AI processing (pre-installed)
- **Error handling**: Check command success and provide helpful error messages
- **Parameter support**: Use `$1`, `$2`, etc. in bash scripts to accept arguments
- **Testing**: Test shortcuts locally before adding to production configs

## Volume Mounts

The docker-compose.yml mounts shortcuts as read-only:

```yaml
volumes:
  - ./.infer/shortcuts:/home/infer/.infer/shortcuts:ro
```

This ensures:

- Shortcuts are available in the CLI
- Container cannot modify your local shortcuts
- Easy to version control your shortcuts

## Next Steps

- Check out other examples in `examples/` directory
- Explore built-in shortcuts in the main `.infer/shortcuts/` directory
- Create shortcuts for your common workflows

## Troubleshooting

**Shortcut not found**:

- Check file is in `.infer/shortcuts/` directory
- Verify YAML syntax is correct
- Restart the CLI container

**AI snippet not working**:

- Ensure API key is configured
- Check prompt formatting and variable names
- Verify JSON output structure

## Cleanup

```bash
docker compose down -v
```

This removes containers and volumes, including persisted conversation data.
