# a2a

**What** - the Agent2Agent anti-corruption layer: discovering remote agents, submitting tasks to them, and polling or reading their results.
**Why** - the A2A protocol and its Go ADK are a foreign model, so they are translated at this boundary
instead of leaking agent cards and task lifecycles into the CLI.
**How** - this package implements the agent-facing tool contracts and owns the SDK dependency; the rest of the CLI only sees normal tool results.

## What it owns

- The A2A client wiring, built on the Inference Gateway ADK (`github.com/inference-gateway/adk`).
- Task submission, capability queries and task-status queries, including streaming updates and artifact retrieval.
- `tools/*.yaml` - manifests for `A2A_SubmitTask`, `A2A_QueryAgent`, `A2A_QueryTask`, including their `modes` and `require_approval`.
- Registering with `internal/container` so local preset agents and remote agents can be started and reached.

## Why it is separate

The protocol integration is grouped under `internal/protocols/` precisely so the SDK stays inside it.
Depguard forbids the ADK anywhere else. That keeps an A2A protocol revision from rippling through the
agent loop.

## How it plugs in

- Configured agents live in `agents.yaml` (`~/.infer/agents.yaml` or a project `.infer/agents.yaml`), and the feature is gated by the `a2a` config section.
- The `A2A_*` tools are added to the registry by this context; the `/a2a` and `/tasks` shortcuts read the same registry.
- Artifact downloads are handled through the `WebFetch` tool with `download=true`.

## Related

- [A2A Agents Configuration](../../../docs/agents-configuration.md)
- [A2A Connections](../../../docs/a2a-connections.md)
- [Tasks Management](../../../docs/tasks-management.md)
