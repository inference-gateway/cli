# agent

**What** - the agent bounded context: the event-driven state machine that turns a user turn into model calls, tool calls and approvals.
**Why** - the loop is the core domain of the CLI, and every other context exists to hand it tools, storage or transport.
**How** - `agent_state_machine.go` routes events, `states/` runs one executor per state, and `domain/` holds the ports the loop calls.

## How it plugs in

- `domain/` is the shared kernel (tool contracts and results, agent mode, chat events) plus the service ports the
  loop calls: skills, GitHub, command hooks, system reminders, user questions, media and image annotation.
- The capabilities implement those ports: `skills` the skills service, `github` the issue and setup services,
  `plugins` a command-hook provider.
- The loop is a conformist to the Inference Gateway SDK: it uses the SDK's message and tool-call types directly,
  so an SDK change reaches the loop without a translation layer.
- `infrastructure/` holds the adapters for the file, frame-source and media ports. The media adapters call the gateway.
- `internal/container/container.go` builds the agent and wires every port.

## Related

- [Plan Mode](../../docs/plan-mode.md)
- [Judge Mode](../../docs/judge-mode.md)
- [Subagents](../../docs/subagents.md)
