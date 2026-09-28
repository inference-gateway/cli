# Tool Approval System

[← Back to README](../README.md)

**What** - the gate that asks you before a sensitive tool (writing files, running commands, scheduling jobs) actually runs.
**Why** - the agent can change your filesystem and run processes, so side-effecting tools stay behind an explicit decision by default.
**How** - every tool inherits the global `tools.safety.require_approval: true` default unless it is
explicitly exempt, and any tool can be overridden with `tools.<name>.require_approval`.

The CLI includes a comprehensive approval system for sensitive tool operations, providing security and
visibility into what actions LLMs are taking.

## How It Works

When a tool requiring approval is executed:

1. **Validation**: Tool arguments are validated
2. **Approval Prompt**: User sees tool details with:
   - Tool name and parameters
   - Real-time diff preview (for file modifications)
   - Approve/Reject/Auto-approve options
3. **Execution**: Tool runs only if approved

## Default Approval Requirements

The global default is `tools.safety.require_approval: true`, so **any tool not explicitly exempt requires
approval**; override per tool with `tools.<name>.require_approval`.

| Tool | Requires Approval | Reason |
| ------ | ------------------- | --------- |
| Write, Edit, MultiEdit, Delete | Yes | Create / modify / remove files |
| Schedule, Agent | Yes | Side effects (scheduled jobs, spawned subprocesses) |
| WebSearch | Yes | Make external requests (global default) |
| WebFetch | No | Explicitly exempt - override with `tools.web_fetch.require_approval` |
| A2A_SubmitTask | Yes | Dispatches work to another agent |
| Bash | Optional | Governed by the per-mode bash allow-list |
| Wait | No | Passive utility - blocks until condition met, no side effects |
| Read, Grep, Tree | No | Read-only operations |
| Memory, TodoWrite | No | Local agent state (explicitly exempt) |
| Image tools | No | Output confined to `~/.infer/projects/<project-slug>/artifacts/` - override with `tools.<name>.require_approval` |
| TextToSpeech | No | Output confined to `text_to_speech.output_dir` - override with `text_to_speech.require_approval` |
| Computer-use tools | No | Run silently in the background |
| A2A_QueryAgent, A2A_QueryTask | No | Read-only A2A queries |

## Approval Configuration

Configure approval requirements per tool:

```bash
# Enable/disable approval for specific tools
infer config set tools.safety.require_approval true   # Global approval
infer config set tools.bash.enabled true              # Enable bash tool
```

Or via configuration file:

```yaml
tools:
  safety:
    require_approval: true  # Global default
  write:
    require_approval: true
  bash:
    require_approval: false  # Override for bash
```

## Approval UI Controls

The approval box is an inline select: **left / right** moves between
**Approve**, **Reject** and **Auto-Approve**, **enter** confirms.

- **Left / Right** - Choose an option
- **Enter** - Confirm the highlighted choice
- **Esc** - Reject execution

## Approval Behaviour

`approval_behaviour` decides how a gated call is resolved, independently of whether it is gated:

- **`prompt`** - ask in the terminal (the default).
- **`ipc`** - route the prompt to a connected client, e.g. the web terminal or an editor integration.
- **`judge`** - hand the decision to an LLM judge (`judge.yaml`); see [Judge Mode](judge-mode.md).
- **`block`** - deny the call outright.

Headless runs block when no approver is reachable. See the
[configuration reference](configuration-reference.md#judge-approval-judgeyaml) for the full matrix.

## Related

- [Tools Reference](tools-reference.md) - per-tool parameters and defaults
- [Channels](channels.md#tool-approval) - how approvals are delivered over Telegram
- [Judge Mode](judge-mode.md) - automated approval decisions
