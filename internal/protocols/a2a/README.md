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
- An agent's `auth` block in `agents.yaml` gives it credentials: a bearer token from an environment variable,
  a file re-read per request or a command (userspace file only, cached until the token expires), or an OIDC
  client-credentials grant. The token endpoint of the grant comes from the security schemes on the agent card.
  `infrastructure.NewClient` adds them to every request to that agent's origin, and a rejection reaches the model
  as an authentication failure naming the agent.
- The gateway is an agent too. An agent URL on the origin of `gateway.url` gets `gateway.api_key` as its bearer
  token, unless `agents.yaml` gives it an `auth` block. The composition root hands the pair over with
  `infrastructure.UseGatewayCredential`. A guardrail refusal from the gateway, a 403 with a JSON-RPC error,
  reaches the model with the policy's message.
- `ReconcileAgents` brings a running chat in line with an edited `agents.yaml` on `/reload`. It reports the
  difference as `AgentChanges`. A removed agent is announced as `AgentStateRemoved` on the status stream once its
  goroutines have exited, and the TUI readiness drops it.
- The `/agents` and `/tasks` shortcuts read the same state.

## Related

- [A2A Agents Configuration](../../../docs/agents-configuration.md)
- [A2A Connections](../../../docs/a2a-connections.md)
- [Tasks Management](../../../docs/tasks-management.md)
