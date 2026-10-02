# agent

**What** - the agent bounded context: what an agent is (its mode, tools, chat events and the ports it calls), the
adapters behind those ports, and how one is spawned out of process.
**Why** - the agent is one concept with two ways to run it. [`loop`](../loop) runs one in process, `runner/` spawns
one as a child. Contexts that only need its model, its adapters or a child depend on this context, never on the loop.
**How** - `domain/` is the shared kernel every context may import. `infrastructure/` adapts its ports to the outside world.
`runner/` runs `infer headless` as a child process.

## How it plugs in

- `domain/` holds the tool contracts and results, agent mode, chat events and the service ports the loop calls.
  The a2a, browser, computer and MCP tools implement its tool contracts.
- `infrastructure/` holds the adapters for the file, frame-source and media ports. The media adapters call the gateway.
- `runner/` spawns `infer headless`, streams its stdout, brokers tool approval over its stdin and returns the final
  assistant message. The `Agent` tool, the scheduler's cron jobs and heartbeat, and the GitHub setup call it.
- `internal/container/container.go` builds the adapters and hands them to the loop.

## Related

- [loop](../loop) - the in-process agent loop
- [Subagents](../../docs/subagents.md)
