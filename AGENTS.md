# AGENTS.md

## Agent Patterns for Inference Gateway CLI

This document outlines agent patterns and best practices for the `infer` CLI tool.

## Agent Command (`infer agent`)

The agent command runs in background mode for autonomous task completion:

- **Syntax**: `infer agent "task description"`
- **Output**: JSON conversation stream
- **Mode**: Iterative problem solving with tool execution
- **Integration**: GitHub issue recognition and SCM workflows

### Usage Examples

```bash
# GitHub issue resolution
infer agent "Fix GitHub issue #38"

# Code improvements
infer agent "Optimize the status command performance"

# Feature implementation
infer agent "Add websocket support to the gateway client"
```

### Agent Workflow

1. **Task Analysis**: Parse and understand the request
2. **Planning**: Break down complex tasks into steps
3. **Execution**: Use tools iteratively to solve problems
4. **Validation**: Test and verify solutions
5. **Completion**: Detect when task objectives are met

### JSON Output Format

```json
{"role": "user", "content": "task description", "timestamp": "2024-01-01T00:00:00Z"}
{"role": "assistant", "content": "analysis", "tool_calls": [...]}
{"role": "tool", "content": "tool result", "tool_call_id": "call_123"}
{"role": "assistant", "content": "next step"}
```

## Chat Mode Agents

Interactive chat mode supports agent-like behaviors:

### Tool Execution

- Direct tool calls: `!!ToolName(arg="value")`
- Bash commands: `!command`
- Auto-completion and validation

### Context Management

- Token tracking per request and session
- History navigation and search
- Model switching mid-conversation

## Agent Configuration

Configure agent behavior through `.infer/config.yaml`:

```yaml
chat:
  default_model: "anthropic/claude-4.1"
  system_prompt: "You are a helpful coding assistant..."

tools:
  enabled: true
  approval_required: false
  sandbox_directories: ["./workspace", "./tmp"]

safety:
  protected_paths: [".infer/", ".git/", "*.env"]
  command_whitelist: ["git", "npm", "go"]
```

## Best Practices

1. **Task Decomposition**: Break complex tasks into manageable steps
2. **Tool Selection**: Choose the right tools for each subtask
3. **Error Handling**: Gracefully handle failures and retry strategies
4. **Token Efficiency**: Optimize prompts and responses for cost
5. **Security**: Validate inputs and respect safety boundaries

## Integration Patterns

### GitHub Integration

- Issue parsing and context extraction
- Branch creation and PR workflows
- Commit message generation

### Development Workflow

- Code analysis and refactoring
- Test generation and execution
- Documentation updates

### Monitoring and Status

- Gateway health checks
- Performance analysis
- Log investigation
