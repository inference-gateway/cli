# A2A behind an authenticated gateway

The Inference Gateway fronts every A2A agent, as in [a2a-gateway](../a2a-gateway/), and applies the same
authentication, guardrails and telemetry to agent traffic that it applies to inference.

```text
                    Keycloak ── issues the token infer presents
                        │
infer ──► inference-gateway /a2a ──┬──► mock-agent                        (tenant mock)
            │  auth, guardrails    └──► mock-agent-b-proxy ► mock-agent-b (tenant mock-b, basic auth)
            ╰──► otel-collector ──► infer traces
```

`infer` knows one agent URL, the gateway's, and that URL is also its `gateway.url`. So it sends `gateway.api_key`
as the bearer token on every A2A request to it.

The model is a [tokenless](https://github.com/inference-gateway/tokenless) mock scripted in
[`scenarios.yaml`](scenarios.yaml), and both agents run the [mock agent](https://github.com/inference-gateway/mock-agent),
which uses a mock LLM. The example needs no API key. You need Docker, [Task](https://taskfile.dev), `curl` and `jq`.

## Requirements

- Inference Gateway v0.58.0 or later, the first release that serves A2A.
- An `infer` release that authenticates A2A calls to the gateway. Until it is released, run `task build` in the
  repository root and pass the build in, for example `task all INFER=../../infer`.

## Layout

```text
a2a-gateway-auth/
├── docker-compose.yaml          # Keycloak, the gateway, two agents, the basic auth proxy and the collector
├── keycloak/realm-export.json   # the realm and the client infer gets its token with
├── policies/                    # the guardrail policies, in Rego
├── Caddyfile                    # basic auth in front of mock-agent-b
├── otel-collector.yaml          # forwards the gateway's spans to infer
├── Taskfile.yml                 # one task per scenario, with the mock model's environment
└── scenarios.yaml               # what the mock model says, per prompt
```

## Run it

```bash
task up
task all
task trace
task metrics
task down
```

| Task | What happens |
| --- | --- |
| `task up` | Starts Keycloak on `localhost:8081` and the gateway on `localhost:8080`, with `mock` and `mock-b` behind it |
| `task token` | Prints an access token from Keycloak's client-credentials grant |
| `task authorized` | With a token, the model delegates to `mock`. The task is accepted and completes |
| `task basic-auth` | The model delegates to `mock-b`, which the gateway reaches with the basic auth from its `A2A_AGENTS` URL |
| `task no-token` | Without a token the gateway answers `401` and the model sees an authentication failure |
| `task pii` | The task description carries a card number. A guardrail policy blocks it and the model sees the policy message |
| `task all` | Runs the four scenarios above |
| `task trace` | Delegates, then shows the session's trace with the gateway's spans nested under the tool call |
| `task metrics` | Shows the gateway's A2A request metrics per agent alias and method |
| `task chat` | Opens `infer chat` with the mock model and a token. Try the prompts from the scenarios |
| `task down` | Stops everything |

Each headless run takes about half a minute, because `infer` waits for late spans before it exits.

### What to expect

`task no-token` and `task pii` print the tool result the model receives:

```text
Authentication failed for A2A agent "localhost:8080": the agent rejected the request with status 401, check gateway.api_key
The request was refused by a guardrail policy: request contains sensitive payment information
```

`task trace` ends with the trace of the session:

```text
session (headless, success)                                      3.9s
├── chat mock/openai/gpt-4o                                      42ms
├── execute_tool A2A_SubmitTask call_0_0                          3ms
│   ├── POST /a2a [inference-gateway, server]                     1ms
│   ╰── POST /a2a [inference-gateway, server]                     2ms
├── chat mock/openai/gpt-4o                                      19ms
╰── chat mock/openai/gpt-4o                                      36ms
```

## Authentication

- The gateway runs with `AUTH_ENABLED` and trusts the Keycloak realm (`AUTH_OIDC_ISSUER`, `AUTH_OIDC_CLIENT_ID`).
  `POST /a2a` then requires a bearer token, the same one inference requires. The agent card stays public.
- The Taskfile fetches a token with `task token` and passes it as `INFER_GATEWAY_API_KEY`. `infer` sends it to the
  gateway's origin only, never to another agent.
- `gateway.api_key` is a fixed value and the token expires after an hour. For long sessions, give the gateway an
  `auth.oidc` block in `agents.yaml` instead. `infer` then fetches and refreshes the token itself, and that block
  takes precedence. See [a2a-auth](../a2a-auth/).
- The gateway never forwards the caller's token to an agent. Per-agent credentials go in the `A2A_AGENTS` URL as
  basic auth, as for `mock-b`. The mock agent accepts bearer tokens only, so a Caddy proxy checks the basic auth
  in front of it.
- `KC_HOSTNAME` pins the issuer to `http://keycloak:8080`, so a token fetched from your machine on port `8081`
  carries the issuer the gateway discovered on the compose network.

## Guardrails

- The gateway runs with `GUARDRAILS_ENABLED` and loads the Rego policies in [`policies/`](policies/). They run
  before and after every non-streaming `/a2a` call.
- [`block_pii.rego`](policies/block_pii.rego) blocks an A2A request whose body contains `4111`. A policy sees the
  raw JSON-RPC request as `input.request.body` and the caller's verified claims as `input.identity`.
- A refusal is a `403` with a JSON-RPC error. `infer` hands its message to the model as the tool error.

## Telemetry

- The gateway runs with `TELEMETRY_ENABLED` and `TELEMETRY_TRACING_ENABLED` and exports its spans to the
  OpenTelemetry Collector, which forwards them to the receiver `infer` opens on port `4319`
  (`INFER_TELEMETRY_RECEIVER_ADDRESS`). `infer` propagates its trace context, so the gateway's spans join the
  session's trace.
- The gateway does not yet pass the trace context on to the agents, so the trace ends at the gateway and the
  agents' spans are not part of it.
- The gateway serves Prometheus metrics on `localhost:9464`. `a2a_request_duration_seconds` counts the A2A
  requests per `alias` and `method`.

## Configuration notes

- `INFER_GATEWAY_MOCK` serves the mock model from inside `infer`. A2A calls still go to the real gateway at
  `INFER_GATEWAY_URL`, with the token.
- `INFER_A2A_TOOLS_SUBMIT_TASK_REQUIRE_APPROVAL=false` lets headless mode submit tasks, since it has no one to ask
  for approval.
- The secrets in `Taskfile.yml`, `docker-compose.yaml`, the `Caddyfile` and the realm export are demo values. Do
  not reuse them.

## See Also

- [A2A Connections, Authenticating to the gateway](../../docs/a2a-connections.md#authenticating-to-the-gateway)
- [a2a-gateway](../a2a-gateway/), the same setup without auth
- [a2a-auth](../a2a-auth/), agents that protect their own `/a2a`
- The gateway's [auth-keycloak](https://github.com/inference-gateway/inference-gateway/tree/main/examples/docker-compose/auth-keycloak),
  [guardrails](https://github.com/inference-gateway/inference-gateway/tree/main/examples/docker-compose/guardrails)
  and [monitoring](https://github.com/inference-gateway/inference-gateway/tree/main/examples/docker-compose/monitoring)
  examples
