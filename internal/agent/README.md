# agent

**What** - the agent bounded context: what an agent is (its mode, tools, chat events and the ports it calls), the
adapters behind those ports, and the two ways to run one.
**Why** - the loop is the core domain of the CLI, and every other context exists to hand it tools, storage or transport.
**How** - `domain/` is the shared kernel every context may import. `infrastructure/` adapts its ports to the outside
world. `loop/` runs an agent in process and `headless/` runs one as an `infer headless` child.

## How it plugs in

- `domain/` holds the tool contracts and results, agent mode, chat events and the service ports the loop calls:
  skills, GitHub, command hooks, system reminders, user questions, media and image annotation. The a2a, browser,
  computer and MCP tools implement its tool contracts.
- The capabilities implement the service ports: `skills` the skills service, `github` the issue and setup services,
  `plugins` a command-hook provider.
- `infrastructure/` holds the adapters for the file, frame-source and media ports. The media adapters call the gateway.
- `loop/` is the event-driven state machine that turns a user turn into model calls, tool calls and approvals.
  `agent_event_driven.go` routes events to `states/`, which runs one executor per state, and `agent_state_machine.go`
  defines the allowed transitions. The loop is a conformist to the Inference Gateway SDK: it uses the SDK's message
  and tool-call types directly, so an SDK change reaches the loop without a translation layer.
- `headless/` spawns `infer headless`, streams its stdout and returns the final assistant message. The `Agent` tool,
  the scheduler's cron jobs and heartbeat, and the GitHub setup call it.
- The tools context never imports `loop/`. depguard enforces it, because the loop consumes tools.
- `internal/container/container.go` builds the loop and wires every port.

## Related

- [Plan Mode](../../docs/plan-mode.md)
- [Judge Mode](../../docs/judge-mode.md)
- [Subagents](../../docs/subagents.md)
