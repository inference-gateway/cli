# A2A behind the gateway

The Inference Gateway fronts every A2A agent, so `infer` knows one agent URL, the gateway's. The gateway merges
the agents' cards into its own and relays each call to the agent the request names, so auth, guardrails and
telemetry apply to agent traffic the same way they apply to inference.

```text
infer ──► inference-gateway /a2a ──┬──► mock-agent    (tenant mock)
                                   └──► mock-agent-b  (tenant mock-b)
```

The gateway names each agent by its alias from `A2A_AGENTS` and advertises it as the `tenant` of one interface on
its card. `A2A_SubmitTask` takes that tenant to pick the agent. The task id comes back as `<alias>:<task id>`, so
polling and follow-ups route on the id alone.

The model is a [tokenless](https://github.com/inference-gateway/tokenless) mock scripted in
[`scenarios.yaml`](scenarios.yaml), and the two agents run the [mock agent](https://github.com/inference-gateway/mock-agent),
which uses a mock LLM. The example needs no API key. You need Docker, [Task](https://taskfile.dev) and `jq`.

## Requirements

- Inference Gateway v0.58.0 or later, the first release that serves A2A.
- `infer` v0.225.0 or later, the first release where `A2A_SubmitTask` takes a `tenant`.

## Layout

```text
a2a-gateway/
├── docker-compose.yaml   # the gateway with A2A_ENABLED and two agents behind it
├── Taskfile.yml          # one task per scenario, with the mock model's environment
└── scenarios.yaml        # what the mock model says, per prompt
```

## Run it

```bash
task up
task card
task all
task down
```

| Task | What happens |
| --- | --- |
| `task up` | Starts the gateway on `localhost:8080` with `mock` and `mock-b` behind it |
| `task card` | Shows the merged card, with every skill prefixed by its alias and one interface per tenant, and the registry |
| `task one-agent` | The model calls `A2A_SubmitTask` with `tenant: mock-b` |
| `task both-agents` | The model delegates to both agents in one turn, through the same URL |
| `task no-tenant` | The model names no tenant. The gateway rejects the call with `-32602` and lists the aliases, and the model retries with one |
| `task all` | Runs the three scenarios above |
| `task execute` | Submits a task to `mock` yourself with `infer tools execute`, without the model |
| `task chat` | Opens `infer chat` with the mock model. Try the prompts from the scenarios |
| `task down` | Stops everything |

The tasks use the `infer` on your `PATH`. To use a local build instead, run `task build` in the repository root and
pass it in, for example `task all INFER=../../infer`.

### What to expect

`task no-tenant` prints the rejected call, the retry and the completion notification, shortened here:

```text
{"content":"A2A task submission failed: ... set params.tenant ... to one of mock, mock-b ... (code: -32602)","role":"tool",...}
{"content":"{\"tool_name\":\"A2A_SubmitTask\",...\"tenant\":\"mock\"},\"success\":true,...\"task_id\":\"mock:cbf43bde-...\",...","role":"tool",...}
{"content":"[A2A Task Completed: mock:cbf43bde-...]\n\n...State: TASK_STATE_COMPLETED...","role":"user",...}
```

## Configuration notes

- `INFER_A2A_AGENTS` lists only the gateway. Add agents to the gateway's `A2A_AGENTS` as `alias=url`, and the model
  finds their tenants on the gateway card through `A2A_QueryAgent`.
- `INFER_A2A_TOOLS_SUBMIT_TASK_REQUIRE_APPROVAL=false` lets headless mode submit tasks, since it has no one to ask
  for approval.
- To use a real model, add a provider key to the gateway service, drop the `INFER_GATEWAY_MOCK*` variables and set
  `INFER_AGENT_MODEL`. `infer` then sends inference through the same gateway on `localhost:8080`, so it fronts both.

## See Also

- [A2A Connections](../../docs/a2a-connections.md)
- [The gateway's own A2A example](https://github.com/inference-gateway/inference-gateway/tree/main/examples/docker-compose/a2a)
- [a2a](../a2a/), the same CLI talking to each agent directly
