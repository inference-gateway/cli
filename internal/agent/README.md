# agent

**What** - the agent bounded context: the event-driven state machine that turns a user turn into model calls, tool calls and approvals.
**Why** - the loop is the most volatile part of the CLI, and every other context exists to hand it tools, storage or transport.
**How** - `internal/container/container.go` constructs it, `states/` runs one executor per state, and `domain/` holds the ports the loop calls.

## What it owns

- `agent_state_machine.go` and `agent_event_driven.go` - the loop and its event routing.
- `agent_streaming.go` - streaming model responses into chat events.
- `states/` - one executor per state: idle, streaming, evaluating tools, approving tools, executing tools, completing, error, cancelled, stopped.
- `judge.go`, `approval_policy.go`, `judge_escalations.go` - approval gates and the LLM judge.
- `hook_commands.go` - shell commands at agent-loop hook points.
- `agent_tools.go` and `llm_tool_service.go` - advertising and executing tools.
- `domain/` - the shared kernel (tool results, agent mode, chat events) plus the service ports the loop needs:
  skills, GitHub issue references, hooks, system reminders, user questions, media and annotations.
- `infrastructure/` - adapters for file, frame-source, image, music, sfx, speech and video services.

## Why it is separate

The loop must not depend on bubbletea, the gateway SDK, Playwright or a storage backend. It talks to
interfaces in `domain/`, so those contexts can change without touching the state machine.

## How it plugs in

- `domain/` imports nothing internal except the shared kernel types, so any context can implement its ports.
- Capabilities without their own `domain/` subpackage (audio, skills, github, channels, plugins) plug in as implementations of these ports.
- `internal/container/container.go` builds the agent; only `cmd/` may import the container.

## Related

- [Plan Mode](../../docs/plan-mode.md)
- [Judge Mode](../../docs/judge-mode.md)
- [Subagents](../../docs/subagents.md)
