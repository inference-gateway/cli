# Tools Reference

[← Back to README](../README.md)

This document provides comprehensive documentation for all tools available to LLMs when tool execution
is enabled in the Inference Gateway CLI.

## Table of Contents

- [Customising Tool Descriptions](#customising-tool-descriptions)
- [File System Tools](#file-system-tools)
  - [Tree Tool](#tree-tool)
  - [Read Tool](#read-tool)
  - [Write Tool](#write-tool)
  - [Edit Tool](#edit-tool)
  - [MultiEdit Tool](#multiedit-tool)
  - [Delete Tool](#delete-tool)
  - [Grep Tool](#grep-tool)
- [Command Execution](#command-execution)
  - [Bash Tool](#bash-tool)
- [Web Tools](#web-tools)
  - [WebSearch Tool](#websearch-tool)
  - [WebFetch Tool](#webfetch-tool)
  - [Github Tool](#github-tool)
- [Workflow Tools](#workflow-tools)
  - [TodoWrite Tool](#todowrite-tool)
  - [RequestPlanApproval Tool](#requestplanapproval-tool)
  - [Schedule Tool](#schedule-tool)
- [Agent-to-Agent Communication](#agent-to-agent-communication)
  - [A2A_SubmitTask Tool](#a2a_submittask-tool)
  - [A2A_QueryAgent Tool](#a2a_queryagent-tool)
  - [A2A_QueryTask Tool](#a2a_querytask-tool)

---

## Customising Tool Descriptions

The description string each tool exposes to the LLM (the prose that
explains what the tool does) is configurable in `.infer/prompts.yaml`
under the `tools` key. This lets you tune phrasing for your model
without recompiling - useful when a model misinterprets a default
description, or when you want to discourage/encourage particular
usage patterns.

```yaml
# .infer/prompts.yaml
tools:
  Bash:
    description: |-
      Execute whitelisted bash commands securely. Only pre-approved
      commands from the whitelist can be executed.
  Read:
    description: |-
      Reads a file from the local filesystem. Always prefer reading
      whole files over chunked reads unless the file is very large.
```

**Rules:**

- Keys use the LLM-visible tool name (e.g. `Bash`, `MultiEdit`,
  `A2A_SubmitTask`, `GetLatestScreenshot`). MCP tools are **not**
  customisable here - their descriptions come from the MCP server.
- Any tool you omit (or any field left empty) falls back to the
  in-code default in `config.DefaultPromptsConfig`. You can override
  one tool without losing the defaults for the rest.
- Environment variable overrides take precedence over the file:
  `INFER_PROMPTS_TOOLS_<UPPER_SNAKE_NAME>_DESCRIPTION` -
  e.g. `INFER_PROMPTS_TOOLS_BASH_DESCRIPTION`,
  `INFER_PROMPTS_TOOLS_A2A_SUBMIT_TASK_DESCRIPTION`.
- Parameter descriptions (the per-argument explanations inside the
  tool schema) are not currently configurable.

---

## File System Tools

### Tree Tool

Display directory structure in a tree format, similar to the Unix `tree` command. Provides a polyfill
implementation when the native `tree` command is unavailable.

**Parameters:**

- `path` (optional): Directory path to display tree structure for (default: current directory)
- `max_depth` (optional): Maximum depth to traverse (unlimited by default)
- `show_hidden` (optional): Whether to show hidden files and directories (default: false)
- `respect_gitignore` (optional): Whether to exclude patterns from .gitignore (default: true)
- `format` (optional): Output format - "text" or "json" (default: "text")

**Examples:**

- Basic tree: Uses current directory with default settings
- Tree with depth limit: `max_depth: 2` - Shows only 2 levels deep
- Tree with hidden files: `show_hidden: true`
- Tree ignoring gitignore: `respect_gitignore: false` - Shows all files including those in .gitignore
- JSON output: `format: "json"` - Returns structured data

**Features:**

- **Native Integration**: Uses system `tree` command when available for optimal performance
- **Polyfill Implementation**: Falls back to custom implementation when `tree` is not installed
- **Pattern Exclusion**: Supports glob patterns to exclude specific files and directories
- **Depth Control**: Limit traversal depth to prevent overwhelming output
- **Hidden File Control**: Toggle visibility of hidden files and directories
- **Multiple Formats**: Text output for readability, JSON for structured data

**Security:**

- Respects configured path exclusions for security
- Validates directory access permissions
- Limited by the same security restrictions as other file tools

---

### Read Tool

Read file content from the filesystem with optional line range specification.

**Configuration:**

```yaml
tools:
  read:
    enabled: true
    require_approval: false  # Read operations don't require approval by default
```

---

### Write Tool

Write content to files on the filesystem with security controls and directory creation support.

**Parameters:**

- `file_path` (required): The path to the file to write
- `content` (required): The content to write to the file
- `create_dirs` (optional): Whether to create parent directories if they don't exist (default: true)
- `overwrite` (optional): Whether to overwrite existing files (default: true)
- `format` (optional): Output format - "text" or "json" (default: "text")

**Features:**

- **Directory Creation**: Automatically creates parent directories when needed
- **Overwrite Control**: Configurable behavior for existing files
- **Security Validation**: Respects path exclusions and security restrictions
- **Performance Optimized**: Efficient file writing with proper error handling

**Security:**

- **Approval Required**: Write operations require approval by default (secure by default)
- **Path Exclusions**: Respects configured excluded paths (e.g., `.infer/` directory)
- **Pattern Matching**: Supports glob patterns for path exclusions
- **Validation**: Validates file paths and content before writing

**Examples:**

- Create new file: `file_path: "output.txt"`, `content: "Hello, World!"`
- Write to subdirectory: `file_path: "logs/app.log"`, `content: "log entry"`, `create_dirs: true`
- Safe overwrite: `file_path: "config.json"`, `content: "{...}"`, `overwrite: false`

**Configuration:**

```yaml
tools:
  write:
    enabled: true
    require_approval: true  # Write operations require approval for security
```

---

### Edit Tool

Perform exact string replacements in files with security validation and preview support.

**Parameters:**

- `file_path` (required): The path to the file to modify
- `old_string` (required): The text to replace (must match exactly)
- `new_string` (required): The text to replace it with
- `replace_all` (optional): Replace all occurrences of old_string (default: false)

**Features:**

- **Indentation-Tolerant Matching**: Exact first; on a miss, a unique leading-whitespace match is re-indented (`strict_whitespace: true` disables)
- **Preview Support**: Shows diff preview before applying changes
- **Atomic Operations**: Either all changes succeed or none are applied
- **Security Validation**: Respects path exclusions and file permissions

**Security:**

- **Read Tool Requirement**: Requires Read tool to be used first on the file
- **Stale-Read Detection**: Rejects the edit if the file changed on disk since the agent last read it, and asks the model to re-read first
- **Approval Required**: Edit operations require approval by default
- **Path Exclusions**: Respects configured excluded paths
- **Validation**: Validates file paths and prevents editing protected files

**Examples:**

- Single replacement: `file_path: "config.txt", old_string: "port: 3000", new_string: "port: 8080"`
- Replace all occurrences: `file_path: "script.py", old_string: "print", new_string: "logging.info", replace_all: true`

**Configuration:**

```yaml
tools:
  edit:
    enabled: true
    require_approval: true  # Edit operations require approval for security
    strict_whitespace: false  # When true, disable the indentation-tolerant fallback (byte-exact only)
```

---

### MultiEdit Tool

Make multiple edits to a single file in atomic operations. All edits succeed or none are applied.

**Parameters:**

- `file_path` (required): The path to the file to modify
- `edits` (required): Array of edit operations to perform sequentially
  - `old_string`: The text to replace (must match exactly)
  - `new_string`: The text to replace it with
  - `replace_all` (optional): Replace all occurrences (default: false)

**Features:**

- **Atomic Operations**: All edits succeed or none are applied
- **Sequential Processing**: Edits are applied in the order provided
- **Indentation-Tolerant Matching**: Each edit matches exactly first, then a unique leading-whitespace fallback (`tools.edit.strict_whitespace`)
- **Preview Support**: Shows comprehensive diff preview
- **Security Validation**: Respects all security restrictions

**Security:**

- **Read Tool Requirement**: Requires Read tool to be used first on the file
- **Stale-Read Detection**: Rejects the edit if the file changed on disk since the agent last read it, and asks the model to re-read first
- **Approval Required**: MultiEdit operations require approval by default
- **Path Exclusions**: Respects configured excluded paths
- **Validation**: Validates all edits before execution

**Example:**

```json
{
  "file_path": "config.yaml",
  "edits": [
    {
      "old_string": "port: 3000",
      "new_string": "port: 8080"
    },
    {
      "old_string": "debug: true",
      "new_string": "debug: false"
    }
  ]
}
```

---

### Delete Tool

Delete files or directories from the filesystem with security controls. Supports wildcard patterns for batch operations.

**Parameters:**

- `path` (required): The path to the file or directory to delete
- `recursive` (optional): Whether to delete directories recursively (default: false)
- `force` (optional): Whether to force deletion (ignore non-existent files, default: false)

**Features:**

- **Wildcard Support**: Delete multiple files using patterns like `*.txt` or `temp/*`
- **Recursive Deletion**: Remove directories and their contents
- **Safety Controls**: Respects configured path exclusions and security restrictions
- **Validation**: Validates file paths and permissions before deletion

**Security:**

- **Approval Required**: Delete operations require approval by default
- **Path Exclusions**: Respects configured excluded paths for security
- **Pattern Matching**: Supports glob patterns for path exclusions
- **Validation**: Validates file paths and prevents deletion of protected directories

**Examples:**

- Delete single file: `path: "temp.txt"`
- Delete directory recursively: `path: "temp/", recursive: true`
- Delete with wildcard: `path: "*.log"`
- Force delete: `path: "missing.txt", force: true`

**Configuration:**

```yaml
tools:
  delete:
    enabled: true
    require_approval: true  # Delete operations require approval for security
```

---

### Grep Tool

A powerful search tool with configurable backend (ripgrep or Go implementation).

**Parameters:**

- `pattern` (required): The regular expression pattern to search for
- `path` (optional): File or directory to search in (default: current directory)
- `output_mode` (optional): Output mode - "content", "files_with_matches", or "count" (default: "files_with_matches")
- `-i` (optional): Case insensitive search
- `-n` (optional): Show line numbers in output
- `-A` (optional): Number of lines to show after each match
- `-B` (optional): Number of lines to show before each match
- `-C` (optional): Number of lines to show before and after each match
- `glob` (optional): Glob pattern to filter files (e.g., "*.js", "*.{ts,tsx}")
- `type` (optional): File type to search (e.g., "js", "py", "rust")
- `multiline` (optional): Enable multiline mode where patterns can span lines
- `head_limit` (optional): Limit output to first N results

**Features:**

- **Dual Backend**: Uses ripgrep when available for optimal performance, falls back to Go implementation
- **Full Regex Support**: Supports complete regex syntax
- **Multiple Output Modes**: Content matching, file lists, or count results
- **Context Lines**: Show lines before and after matches
- **File Filtering**: Filter by glob patterns or file types
- **Multiline Matching**: Patterns can span multiple lines
- **Automatic Exclusions**: Automatically excludes common directories and files (.git, node_modules, .infer, etc.)
- **Gitignore Support**: Respects .gitignore patterns in your repository
- **User-Configurable Exclusions**: Additional exclusion patterns can be configured by users (not by the LLM)

**Security & Exclusions:**

- **Path Exclusions**: Respects configured excluded paths and patterns
- **Automatic Exclusions**: The tool automatically excludes:
  - Version control directories (.git, .svn, etc.)
  - Dependency directories (node_modules, vendor, etc.)
  - Build artifacts (dist, build, target, etc.)
  - Cache and temp files (.cache, \*.tmp, \*.log, etc.)
  - Security-sensitive files (.env, secrets, etc.)
- **Gitignore Integration**: Automatically reads and respects .gitignore patterns
- **Validation**: Validates search patterns and file access
- **Performance Limits**: Configurable result limits to prevent overwhelming output

**Examples:**

- Basic search: `pattern: "error", output_mode: "content"`
- Case insensitive: `pattern: "TODO", -i: true, output_mode: "content"`
- With context: `pattern: "function", -C: 3, output_mode: "content"`
- File filtering: `pattern: "interface", glob: "*.go", output_mode: "files_with_matches"`
- Count results: `pattern: "log.*Error", output_mode: "count"`

**Configuration:**

```yaml
tools:
  grep:
    enabled: true
    backend: auto  # "auto", "ripgrep", or "go"
    require_approval: false
```

---

## Command Execution

### Bash Tool

Execute whitelisted bash commands securely with validation against configured command patterns.

**Configuration:**

```yaml
tools:
  bash:
    enabled: true
    whitelist:
      commands:  # Exact command matches
        - ls
        - pwd
        - git status
      patterns:  # Regex patterns for complex commands
        - ^git branch.*
        - ^npm (install|test|run).*
    require_approval: false  # Can be set to true for additional security
```

**Security:**

- Only whitelisted commands and patterns can be executed
- Commands are validated before execution
- Supports both exact matches and regex patterns

**Restricted operators:**

The matcher is shell-aware, so a whitelisted command cannot be composed into something more
dangerous:

- **Pipes and chains** (`|`, `&&`, `||`, `;`) are split at the top level and **every segment must be
  independently whitelisted**. `ls | head` is allowed only if both `ls` and `head` are; `ls | xargs rm`
  is rejected because `xargs rm` is not.
- **File-write redirections** (`>`, `>>`, `&>file`, `>&file`) are **blocked by default**, even for a
  whitelisted command (`echo secret > /etc/passwd` is rejected). They are allowed only when a whitelist
  **pattern** matches the *entire* command - a prefix pattern such as `^git log` will not unlock
  `git log > file`.
- **Benign redirections** that only discard or merge streams (`2>&1`, `>/dev/null`, `2>/dev/null`) are
  stripped before matching and remain allowed.
- **Command substitution** (`$(...)`, backticks, `<(...)`, `>(...)`) is always rejected.

To deliberately allow a redirection, add an anchored pattern (`^...$`) that covers the whole command:

```yaml
tools:
  bash:
    whitelist:
      patterns:
        - ^echo .+ >> /var/log/app\.log$   # allow appending to one specific file
```

A rejected command returns explanatory feedback naming the restricted operator and how to whitelist
it, and (when approval is enabled) still goes through the normal approval prompt.

---

## Web Tools

### WebSearch Tool

Search the web using DuckDuckGo or Google search engines to find information.

**Configuration:**

```yaml
tools:
  web_search:
    enabled: true
    default_engine: duckduckgo
    max_results: 10
    engines:
      - duckduckgo
      - google
    timeout: 10
```

---

### WebFetch Tool

Fetch content from whitelisted URLs or GitHub references using the format `example.com`.

**Configuration:**

```yaml
tools:
  web_fetch:
    enabled: true
    whitelisted_domains:
      - golang.org
      - github.com
    safety:
      max_size: 8192  # 8KB
      timeout: 30
      allow_redirect: true
    cache:
      enabled: true
      ttl: 3600  # 1 hour
```

---

### Github Tool

Interact with GitHub API to fetch issues, pull requests, create/update comments, and create pull
requests with authentication support. This is a standalone tool separate from WebFetch.

**Parameters:**

- `owner` (required): Repository owner (username or organization)
- `repo` (required): Repository name
- `resource` (optional): Resource type to fetch or create (default: "issue")
  - `issue`: Fetch a specific issue
  - `issues`: Fetch a list of issues
  - `pull_request`: Fetch a specific pull request
  - `comments`: Fetch comments for an issue/PR
  - `create_comment`: Create a comment on an issue/PR
  - `update_comment`: Update an existing comment
  - `create_pull_request`: Create a new pull request
- `issue_number` (required for issue/pull_request/comments/create_comment): Issue or PR number
- `comment_id` (required for update_comment): Comment ID to update
- `comment_body` (required for create_comment and update_comment): Comment body text
- `title` (required for create_pull_request): Pull request title
- `body` (optional for create_pull_request): Pull request body/description
- `head` (required for create_pull_request): Head branch name
- `base` (optional for create_pull_request): Base branch name (default: "main")
- `state` (optional): Filter by state for issues list ("open", "closed", "all", default: "open")
- `per_page` (optional): Number of items per page for lists (1-100, default: 30)

**Features:**

- **GitHub API Integration**: Direct access to GitHub's REST API v3
- **Authentication**: Supports GitHub personal access tokens via environment variables
- **Multiple Resources**: Fetch issues, pull requests, comments, and create new content
- **Structured Data**: Returns properly typed GitHub data structures
- **Error Handling**: Comprehensive error handling with GitHub API error messages
- **Rate Limiting**: Respects GitHub API rate limits
- **Security**: Configurable timeout and response size limits
- **Environment Variables**: Supports token resolution via `%GITHUB_TOKEN%` syntax
- **Security Controls**: Owner validation for secure repository access

**Configuration:**

```yaml
tools:
  github:
    enabled: true
    token: "%GITHUB_TOKEN%"  # Environment variable reference
    base_url: "https://api.github.com"
    owner: "your-username"  # Default owner for security
    repo: "your-repo"       # Default repository (optional)
    safety:
      max_size: 1048576  # 1MB
      timeout: 30        # 30 seconds
    require_approval: false
```

**Examples:**

- Fetch specific issue: `owner: "octocat", repo: "Hello-World", resource: "issue", issue_number: 1`
- List open issues: `owner: "octocat", repo: "Hello-World", resource: "issues", state: "open", per_page: 10`
- Fetch pull request: `owner: "octocat", repo: "Hello-World", resource: "pull_request", issue_number: 5`
- Get issue comments: `owner: "octocat", repo: "Hello-World", resource: "comments", issue_number: 1`
- Create comment: `owner: "octocat", repo: "Hello-World", resource: "create_comment",
  issue_number: 1, comment_body: "Great work!"`
- Update comment: `owner: "octocat", repo: "Hello-World", resource: "update_comment",
  comment_id: 12345, comment_body: "Updated: Great work with improvements!"`
- Create pull request: `owner: "octocat", repo: "Hello-World", resource: "create_pull_request",
  title: "Add feature", body: "New feature implementation", head: "feature-branch", base: "main"`

---

## Workflow Tools

### TodoWrite Tool

Create and manage structured task lists for LLM-assisted development workflows.

**Parameters:**

- `todos` (required): Array of todo items with status tracking
  - `id` (required): Unique identifier for the task
  - `content` (required): Task description
  - `status` (required): Task status - "pending", "in_progress", or "completed"

**Features:**

- **Structured Task Management**: Organized task tracking with status
- **Real-time Updates**: Mark tasks as in_progress/completed during execution
- **Progress Tracking**: Visual representation of task completion
- **LLM Integration**: Designed for LLM-assisted development workflows

**Security:**

- **No File System Access**: Pure memory-based operation
- **Validation**: Validates todo structure and status values
- **Size Limits**: Configurable limits on todo list size

**Example:**

```json
{
  "todos": [
    {
      "id": "1",
      "content": "Update README with new tool documentation",
      "status": "in_progress"
    },
    {
      "id": "2",
      "content": "Add test cases for new features",
      "status": "pending"
    }
  ]
}
```

**Configuration:**

```yaml
tools:
  todo_write:
    enabled: true
    require_approval: false
```

### RequestPlanApproval Tool

Submits a finalized plan for user approval and persists it as a
Markdown file under `<configDir>/plans/`. Available only when the agent
is in **Plan Mode** (toggle via Shift+Tab in the chat TUI).

> **📖 For the full plan-mode workflow, see [Plan Mode Guide](plan-mode.md).**

**How it works:**

- The plan is written atomically to `<configDir>/plans/<YYYY-MM-DD-HHMMSS>-<slug>.md` (e.g. `~/.infer/plans/2026-04-27-103015-add-login-flow.md`).
- The chat TUI shows the rendered plan with **Accept**, **Reject**, and **Accept & Auto-Approve** options.
- On accept, the agent switches out of plan mode and executes the plan.
- On reject, the file remains on disk as an audit trail; the user can reply with feedback and the agent re-iterates.
- The LLM is instructed to ask clarifying questions in normal assistant turns first, and only call this tool when the plan is complete.

**Parameters:**

- `title` (required): A short human-readable phrase (≤ 60 chars, no
  slashes or `..`). Becomes the H1 heading of the saved file and the
  basis of the filename slug.
- `plan` (required): The full plan as Markdown. Use H2 sections in this
  order - `## Context`, `## Files to Modify`, `## Current Code`,
  `## Changes`, `## Performance Impact`, `## Critical Files`,
  `## Edge Cases`, `## Verification`. Omit any section that is not
  applicable.

**Example:**

```json
{
  "title": "Add login flow",
  "plan": "## Context\n\nUsers can't sign in...\n\n## Files to Modify\n\n- internal/auth/login.go - add handler\n\n## Verification\n\nRun `task test`."
}
```

**Configuration:**

The tool is auto-registered and always enabled in plan mode. No config knobs.

### Schedule Tool

Create recurring or one-off tasks that the agent runs on a cron schedule and
delivers back through the messaging channel that triggered the current session
(e.g. Telegram). Useful for "send me X every morning" or "remind me at 6pm
today to call mum" - initiated from a chat with the bot.

> **📖 For an end-to-end walkthrough, see [Scheduling Guide](scheduling.md).**

**How it works:**

- Each scheduled job is persisted as a YAML file under `~/.infer/schedules/`.
- The `infer channels-manager` daemon hosts the scheduler and watches that directory via fsnotify, so newly created jobs fire without a restart.
- Each fire spawns a brand-new `infer agent` session - no context carries between runs. Make prompts specific and self-contained.
- Channel + recipient are derived automatically from the current session ID -
  the LLM never passes them. The tool can therefore only be used from a
  channel-driven session.
- One-off jobs (`run_once: true`) are deleted automatically after their first fire.

**Disabled by default.** Enable in config under `tools.schedule.enabled: true`.

**Parameters:**

- `operation` (required): One of `create`, `list`, `get`, `update`, `delete`.
- `job_id`: Required for `get`, `update`, `delete`.
- `cron_expression`: Required for `create`. Standard 5-field crontab or `@every <duration>`.
- `prompt`: Required for `create`. The task to give the agent on each fire.
- `run_once` (optional, default `false`): When true, the job is deleted after its first fire.
- `name`, `description`, `model`: Optional metadata; `model` overrides `agent.model` for that job.

The LLM is instructed to **always confirm with the user whether they want a
one-off or recurring job** before creating one - there is no safe default
for that decision.

**Example - recurring:**

```json
{
  "operation": "create",
  "cron_expression": "0 8 * * *",
  "prompt": "Find an inspiring quote for today and respond with the quote and its author. Keep it under 3 sentences.",
  "name": "Daily morning quote"
}
```

**Example - one-off reminder:**

```json
{
  "operation": "create",
  "cron_expression": "0 18 26 4 *",
  "prompt": "Remind me to call mum.",
  "run_once": true,
  "name": "Call mum reminder"
}
```

**Example - list:**

```json
{ "operation": "list" }
```

**Example - delete:**

```json
{ "operation": "delete", "job_id": "0a1b2c3d-..." }
```

**Configuration:**

```yaml
tools:
  schedule:
    enabled: false              # disabled by default
    require_approval: true      # require approval by default
    storage_dir: ""             # default: ~/.infer/schedules
    max_jobs: 100
```

**Security:**

- **Approval required by default** - the LLM cannot create/modify schedules without user confirmation.
- **Channel must be configured** - the tool refuses to schedule for channels that aren't enabled in `channels.<name>.enabled`.
- **Channel-session only** - the tool errors out if invoked from chat mode or any non-channel session, since it has no recipient to route to.
- **Daemon-bound execution** - jobs only fire while `infer channels-manager` is running.

---

## Agent-to-Agent Communication

The A2A (Agent-to-Agent) tools enable communication between the CLI client and specialized A2A server
agents, allowing for task delegation, distributed processing, and agent coordination.

> **📖 For detailed configuration instructions, see [A2A Agents Configuration Guide](agents-configuration.md)**

### A2A_SubmitTask Tool

Submit tasks to specialized A2A agents for distributed processing.

**Parameters:**

- `agent_url` (required): URL of the A2A agent server
- `task_description` (required): Description of the task to perform
- `metadata` (optional): Additional task metadata as key-value pairs

**Features:**

- **Task Delegation**: Submit complex tasks to specialized agents
- **Streaming Responses**: Real-time task execution updates
- **Metadata Support**: Include contextual information with tasks
- **Task Tracking**: Automatic tracking of submitted tasks with IDs
- **Error Handling**: Comprehensive error reporting and retry logic

**Examples:**

- Code analysis: `agent_url: "http://security-agent:8080", task_description: "Analyze codebase for security vulnerabilities"`
- Documentation: `agent_url: "http://docs-agent:8080", task_description: "Generate API documentation"`
- Testing: `agent_url: "http://test-agent:8080", task_description: "Create unit tests for UserService class"`

---

### A2A_QueryAgent Tool

Retrieve agent capabilities and metadata for discovery and validation.

**Parameters:**

- `agent_url` (required): URL of the A2A agent to query

**Features:**

- **Agent Discovery**: Query agent capabilities and supported task types
- **Health Checks**: Verify agent availability and status
- **Metadata Retrieval**: Get agent configuration and feature information
- **Connection Validation**: Test connectivity before task submission

**Examples:**

- Capability check: `agent_url: "http://agent:8080"` - Returns agent card with available features
- Health status: Query agent before submitting critical tasks

---

### A2A_QueryTask Tool

Query the status and results of previously submitted tasks.

**Parameters:**

- `agent_url` (required): URL of the A2A agent server
- `context_id` (required): Context ID for the task
- `task_id` (required): ID of the task to query

**Features:**

- **Status Monitoring**: Check task completion status and progress
- **Result Retrieval**: Access task outputs and generated content
- **Error Diagnostics**: Get detailed error information for failed tasks
- **Artifact Discovery**: List available artifacts from completed tasks

**Examples:**

- Status check: `agent_url: "http://agent:8080", context_id: "ctx-123", task_id: "task-456"`
- Result access: Retrieve task outputs and completion details

---

## A2A Workflow Example

```text
1. Query agent capabilities:        A2A_QueryAgent
2. Submit task for processing:      A2A_SubmitTask
3. Monitor task progress:           A2A_QueryTask
```

## A2A Configuration

A2A tools are configured in the tools section:

```yaml
a2a:
  enabled: true
  tools:
    submit_task:
      enabled: true
      require_approval: true
    query_agent:
      enabled: true
      require_approval: false
    query_task:
      enabled: true
      require_approval: false
```

**Note:** Artifact downloads from A2A tasks are handled via the WebFetch tool with `download=true`.
Files are automatically saved to `<configDir>/tmp` with the filename extracted from the download URL.

## A2A Use Cases

- **Code Analysis**: Submit codebases to security or quality analysis agents
- **Documentation Generation**: Generate API docs, README files, or technical documentation
- **Testing**: Create comprehensive test suites with specialized testing agents
- **Data Processing**: Process large datasets with specialized data analysis agents
- **Content Creation**: Generate content with specialized writing or design agents

For detailed A2A documentation and examples, see [A2A Agents Configuration Guide](agents-configuration.md).

---

[← Back to README](../README.md)
