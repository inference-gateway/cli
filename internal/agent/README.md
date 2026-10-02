# agent

**What** - the agent bounded context: what an agent is (its mode, tools, chat events and the ports it calls) and the adapters behind those ports.
**Why** - the agent is one concept with two ways to run it. [`loop`](../loop) runs one in process, and the contexts
that only need its model or its adapters depend on this context, never on the loop.
**How** - `domain/` is the shared kernel every context may import. `infrastructure/` adapts its ports to the outside world.

## How it plugs in

- `domain/` holds the tool contracts and results, agent mode, chat events and the service ports the loop calls.
  The a2a, browser, computer and MCP tools implement its tool contracts.
- `infrastructure/` holds the adapters for the file, frame-source and media ports. The media adapters call the gateway.
- `internal/container/container.go` builds the adapters and hands them to the loop.

## Related

- [loop](../loop) - the in-process agent loop
- [Subagents](../../docs/subagents.md)
