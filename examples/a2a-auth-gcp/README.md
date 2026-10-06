# A2A agent behind Google Cloud identity

The same setup as [a2a-auth](../a2a-auth/), with Google as the issuer instead of Keycloak. Read that example
first: it explains how the agent verifies tokens and how `agents.yaml` carries credentials, none of which changes here.

```text
infer ──► mock-agent  (A2A_AUTH_ISSUER_URL=https://accounts.google.com)
  ▲
  ╰── .token, written by get-token.sh from gcloud
```

No identity-provider setup is needed. A Google service account can mint an OIDC ID token for any audience you
name, which is how Google's own services authenticate service-to-service calls on Cloud Run. There is no
client-credentials grant and no client secret: `infer` reads the token from a file with `token_file`, the shape a
workload gets from the metadata server or a sidecar.

The model is a [tokenless](https://github.com/inference-gateway/tokenless) mock scripted in
[`scenarios.yaml`](scenarios.yaml), so the example needs no LLM key. You need Docker, [Task](https://taskfile.dev)
and the [gcloud CLI](https://cloud.google.com/sdk/docs/install) logged in.

## Setup

```bash
PROJECT_ID=$(gcloud config get-value project)
SA="infer-a2a-client@${PROJECT_ID}.iam.gserviceaccount.com"

gcloud iam service-accounts create infer-a2a-client
# Let your own account mint tokens as the service account.
gcloud iam service-accounts add-iam-policy-binding "$SA" \
  --member="user:$(gcloud config get-value account)" \
  --role=roles/iam.serviceAccountTokenCreator

cat > .env <<ENV
GCP_SERVICE_ACCOUNT=$SA
AGENT_AUDIENCE=https://gcp-agent.example.com
ENV
```

## Run it

```bash
task up
task all
task down
```

| Task | What happens |
| --- | --- |
| `task up` | Starts the agent on `localhost:8093`, trusting `https://accounts.google.com` for `AGENT_AUDIENCE` |
| `task token` | Runs `get-token.sh` and writes the ID token to `.token`, which `agents.yaml` points at |
| `task delegate` | The model delegates to the agent. The task is accepted and completes |
| `task wrong-token` | Overwrites `.token` with junk. The agent answers `401` and the model sees an authentication failure |
| `task all` | Runs the three above |
| `task chat` | Opens `infer chat` with the mock model. Try "ask the gcp agent to echo hello" |
| `task down` | Stops the agent |

## Configuration notes

- [`.infer/agents.yaml`](.infer/agents.yaml) uses `token_file: .token`. A project `agents.yaml` cannot run
  commands, so the token is written by `task token`. In your own `~/.infer/agents.yaml` skip the file and let
  `infer` mint and refresh the token itself:

  ```yaml
  auth:
    token_command: [gcloud, auth, print-identity-token, --impersonate-service-account=<SA>, --audiences=https://gcp-agent.example.com]
  ```

  On GCP itself, with the service account attached to the workload, drop `--impersonate-service-account`, or read
  the metadata server's `identity?audience=` endpoint into a file.
- **The audience is whatever you say it is.** `AGENT_AUDIENCE` must be identical in `.env` (read by the agent)
  and in the token request (read by `get-token.sh`).
- **User credentials cannot set an audience.** `gcloud auth print-identity-token` without impersonation returns a
  token for gcloud's own client ID, which the agent rejects.
- **Any Google account can pass authentication.** `https://accounts.google.com` is one issuer for every Google
  account, so a valid token only proves "some Google identity minted a token for your audience". The mock agent
  has no policy layer. Put the agent behind the gateway and add a guardrail on `input.identity.email`, as the
  gateway's [auth-gcp example](https://github.com/inference-gateway/inference-gateway/tree/main/examples/docker-compose/auth-gcp)
  does, before exposing a real agent this way.
- ID tokens live one hour. `task token` again when `task delegate` starts failing.

## See Also

- [A2A Agents Configuration, Authentication](../../docs/agents-configuration.md#authentication)
- [a2a-auth](../a2a-auth/), the same agent behind a static token and behind Keycloak
- [a2a-auth-entraid](../a2a-auth-entraid/) and [a2a-auth-aws](../a2a-auth-aws/)
- [Authenticate your AI agents on Cloud Run](https://docs.cloud.google.com/run/docs/ai/authenticate-agents)
