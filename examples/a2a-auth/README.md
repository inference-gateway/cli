# A2A agents behind authentication

Two A2A agents protect their `/a2a` endpoint, and `infer` authenticates to each with the credentials from
[`.infer/agents.yaml`](.infer/agents.yaml):

```text
infer ──┬──► mock-agent-token  (static bearer token)
        │
        └──► mock-agent-oidc   (OIDC)
                 ▲
     Keycloak ───┘  issues the token infer presents
```

- **token-agent** is started with `A2A_AUTH_TOKEN`. `infer` sends that token, read from `TOKEN_AGENT_TOKEN`.
- **oidc-agent** is started with `A2A_AUTH_ENABLED` and trusts a Keycloak realm. `infer` fetches a token from
  Keycloak with the client-credentials grant, using the client secret from `OIDC_AGENT_CLIENT_SECRET`, reuses it
  and refreshes it before it expires.

`agents.yaml` only names the environment variables. The secrets themselves are set in the
[`Taskfile.yml`](Taskfile.yml), which stands in for your shell or secret manager.

The model is a [tokenless](https://github.com/inference-gateway/tokenless) mock scripted in
[`scenarios.yaml`](scenarios.yaml), and both agents run the [mock agent](https://github.com/inference-gateway/mock-agent),
which uses a mock LLM. The example needs no API key. You need Docker and [Task](https://taskfile.dev).

## Requirements

- An `infer` release with per-agent credentials (`auth` in `agents.yaml`). Until it is released, run `task build`
  in the repository root and pass the build in, for example `task all INFER=../../infer`.

## Layout

```text
a2a-auth/
├── .infer/agents.yaml             # the two agents and where their credentials come from
├── docker-compose.yaml            # both agents and Keycloak
├── keycloak/realm-export.json     # the realm, the infer client and its audience mapper
├── Taskfile.yml                   # one task per scenario, with the secrets in its environment
└── scenarios.yaml                 # what the mock model says, per prompt
```

## Run it

```bash
task up
task status
task all
task down
```

| Task | What happens |
| --- | --- |
| `task up` | Starts the token agent on `localhost:8091`, Keycloak on `localhost:8090` and the OIDC agent on `localhost:8092` |
| `task status` | Probes both agents with their credentials |
| `task token` | The model delegates to the token agent. The task is accepted and completes |
| `task oidc` | The model delegates to the OIDC agent with a token from Keycloak. The task is accepted and completes |
| `task wrong-token` | A wrong bearer token. The agent answers `401` and the model sees an authentication failure |
| `task missing-token` | `TOKEN_AGENT_TOKEN` is empty. The failure names the variable and no request is sent |
| `task wrong-secret` | A wrong client secret. Keycloak refuses to issue a token and the model sees an authentication failure |
| `task all` | Runs the five scenarios above |
| `task chat` | Opens `infer chat` with the mock model. Try the prompts from the scenarios |
| `task down` | Stops everything |

### What to expect

`task wrong-token`, `task missing-token` and `task wrong-secret` print the tool result the model receives:

```text
Authentication failed for A2A agent "token-agent": the agent rejected the request with status 401, check its auth settings in agents.yaml
Authentication failed for A2A agent "token-agent": environment variable TOKEN_AGENT_TOKEN is not set
Authentication failed for A2A agent "oidc-agent": fetching the OIDC token: the token endpoint answered with status 401 unauthorized_client
```

The last two take a few seconds, because the request is retried before the failure is reported.

## Configuration notes

- The A2A protocol has an agent declare how to authenticate in the `securitySchemes` of its agent card, and
  `infer` reads the OIDC token endpoint from there. The mock agent does not declare them yet, so
  `auth.oidc.issuer_url` names the issuer instead. For an agent that does declare them, `issuer_url` is optional
  and pins the issuer the card may point at.
- Keycloak and the OIDC agent share one network namespace (`network_mode: service:keycloak`), so the issuer is
  `http://localhost:8090/realms/a2a` for `infer` on your machine and for the agent in its container. The token's
  issuer has to match on both sides.
- The realm's `infer` client has a service account and an audience mapper that adds `mock-agent` to the token,
  which is the audience the OIDC agent accepts (`A2A_AUTH_AUDIENCE`).
- `INFER_A2A_TOOLS_SUBMIT_TASK_REQUIRE_APPROVAL=false` lets headless mode submit tasks, since it has no one to ask
  for approval.
- The secrets in `Taskfile.yml`, `docker-compose.yaml` and the realm export are demo values. Do not reuse them.

## See Also

- [A2A Agents Configuration, Authentication](../../docs/agents-configuration.md#authentication)
- [A2A Connections](../../docs/a2a-connections.md)
- [a2a-gateway](../a2a-gateway/), every agent behind the gateway
- [The gateway's Keycloak example](https://github.com/inference-gateway/inference-gateway/tree/main/examples/docker-compose/auth-keycloak)
