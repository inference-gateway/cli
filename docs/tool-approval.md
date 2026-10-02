# Tool Approval System

[← Back to README](../README.md)

**What** - the gate that asks you before a sensitive tool (writing files, running commands, scheduling jobs) actually runs.
**Why** - the agent can change your filesystem and run processes, so side-effecting tools stay behind an explicit decision by default.
**How** - a tool takes `require_approval` from its config section, then its manifest, then the global
`tools.safety.require_approval: true` default. Computer-use tools and Bash have their own gates. The per-tool
defaults and the full resolution order are in the [Tools Reference](tools-reference.md#tool-overview).
These settings live in `~/.infer/tools.yaml`, not `config.yaml` - edit that file directly, since
`infer config set tools.*` is rejected.

## How It Works

When a tool requiring approval is executed:

1. **Validation**: Tool arguments are validated
2. **Approval Prompt**: User sees tool details with:
   - Tool name and parameters
   - Real-time diff preview (for file modifications)
   - Approve/Reject/Auto-approve options
3. **Execution**: Tool runs only if approved

Bash is the exception: it is gated by the per-mode bash allow-list, not by `require_approval`.

## Approval UI Controls

The approval box is an inline select: **left / right** moves between
**Approve**, **Reject** and **Auto-Approve**, **enter** confirms.

- **Left / Right** - Choose an option
- **Enter** - Confirm the highlighted choice
- **Esc** - Reject execution

## Approval Behaviour

`tools.safety.approval_behaviour` decides how a gated call is resolved (`prompt`, `ipc`, `judge` or `block`),
independently of whether it is gated. It is global: there is no per-tool behaviour. Headless runs block when no
approver is reachable. See the [configuration reference](configuration-reference.md#tool-settings) for the values
and the per-tool `require_approval` overrides.

## Related

- [Tools Reference](tools-reference.md#tool-overview) - per-tool approval defaults
- [Channels](channels.md#tool-approval) - how approvals are delivered over Telegram
- [Judge Mode](judge-mode.md) - automated approval decisions
