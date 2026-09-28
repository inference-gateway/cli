# a2a

**What** - the Agent2Agent anti-corruption layer: discovering remote agents, submitting tasks to them, and polling their status and artifacts.
**Why** - the A2A protocol and its Go ADK are a foreign model, so they are translated here instead of leaking agent
cards and task lifecycles into the CLI.
**How** - `infrastructure/` wraps the ADK client and agent cards, `domain/` holds the CLI's own agent and task model,
and the tools return normal tool results.

## How it plugs in

- `NewTools(...)` builds `A2A_SubmitTask`, `A2A_QueryAgent` and `A2A_QueryTask` from the manifests in `tools/`.
  Their approval is set under `a2a.tools.*`.
- Submitted tasks are tracked by the job supervisor and polled until they finish. `A2A_SubmitTask` downloads the
  artifacts itself when `a2a.task.artifacts_auto_download` is on. Otherwise it tells the model to fetch them with `WebFetch`.
- `AgentSupervisor` starts the configured local agent containers. Agents are declared in `agents.yaml`, and the
  feature is gated by the `a2a` config section.
- The `/agents` and `/tasks` shortcuts read the same state.

## Related

- [A2A Agents Configuration](../../../docs/agents-configuration.md)
- [A2A Connections](../../../docs/a2a-connections.md)
- [Tasks Management](../../../docs/tasks-management.md)
