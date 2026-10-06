# A2A agent behind Amazon Cognito

The same setup as [a2a-auth](../a2a-auth/), with a Cognito user pool as the issuer instead of Keycloak. Read that
example first: it explains how the agent verifies tokens and how `agents.yaml` carries credentials, none of which
changes here.

```text
infer ──► mock-agent  (A2A_AUTH_ISSUER_URL=https://cognito-idp.<region>.amazonaws.com/<pool>)
  ▲
  ╰── .token, written by get-token.sh from the AWS CLI
```

Cognito issues machine-to-machine tokens to an _app client_ that holds a secret. Those tokens carry no `aud`
claim (Cognito binds audiences only for user logins), so the agent checks the `client_id` claim against
`A2A_AUTH_AUDIENCE` instead, the check AWS documents for resource servers. `infer` reads the token from a file with
`token_file`, the shape a workload gets from a sidecar or an init step.

The model is a [tokenless](https://github.com/inference-gateway/tokenless) mock scripted in
[`scenarios.yaml`](scenarios.yaml), so the example needs no LLM key. You need Docker, [Task](https://taskfile.dev)
and the [AWS CLI](https://aws.amazon.com/cli/) with credentials that can manage Cognito.

## Setup

```bash
AWS_REGION=eu-west-1
POOL_ID=$(aws cognito-idp create-user-pool --region "$AWS_REGION" \
  --pool-name infer-a2a --query UserPool.Id --output text)

# Machine clients must be allowed at least one custom scope from a resource server.
aws cognito-idp create-resource-server --region "$AWS_REGION" --user-pool-id "$POOL_ID" \
  --identifier infer-a2a-agent --name "Infer A2A agent" \
  --scopes ScopeName=invoke,ScopeDescription="Submit tasks to the agent"

CLIENT_ID=$(aws cognito-idp create-user-pool-client --region "$AWS_REGION" --user-pool-id "$POOL_ID" \
  --client-name infer-a2a-client --generate-secret \
  --explicit-auth-flows ALLOW_CLIENT_TOKEN_AUTH \
  --allowed-o-auth-scopes infer-a2a-agent/invoke \
  --query UserPoolClient.ClientId --output text)
CLIENT_SECRET=$(aws cognito-idp describe-user-pool-client --region "$AWS_REGION" --user-pool-id "$POOL_ID" \
  --client-id "$CLIENT_ID" --query UserPoolClient.ClientSecret --output text)

cat > .env <<ENV
AWS_REGION=$AWS_REGION
COGNITO_USER_POOL_ID=$POOL_ID
COGNITO_CLIENT_ID=$CLIENT_ID
COGNITO_CLIENT_SECRET=$CLIENT_SECRET
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
| `task up` | Starts the agent on `localhost:8093`, trusting the user pool's issuer for the app client ID |
| `task token` | Runs `get-token.sh` (`aws cognito-idp get-client-token`) and writes the token to `.token` |
| `task delegate` | The model delegates to the agent. The task is accepted and completes |
| `task wrong-token` | Overwrites `.token` with junk. The agent answers `401` and the model sees an authentication failure |
| `task all` | Runs the three above |
| `task chat` | Opens `infer chat` with the mock model. Try "ask the aws agent to echo hello" |
| `task down` | Stops the agent |

## Configuration notes

- [`.infer/agents.yaml`](.infer/agents.yaml) uses `token_file: .token`. A project `agents.yaml` cannot run
  commands, so the token is written by `task token`. In your own `~/.infer/agents.yaml` let `infer` fetch and
  refresh it:

  ```yaml
  auth:
    token_command: [/path/to/a2a-auth-aws/get-token.sh]
  ```

  With a user pool domain configured, the `oidc` block works too: the agent card's discovery document names the
  token endpoint and `client_secret_env` holds the app client secret.
- **No `aud` claim.** Decode the token and you find `client_id`, `token_use: access` and `scope`, but no `aud`.
  That is expected, and why `A2A_AUTH_AUDIENCE` is the app client ID.
- **`get-client-token` needs `ALLOW_CLIENT_TOKEN_AUTH`.** An app client created for user sign-in flows cannot use
  it; create a separate machine client.
- **Bedrock AgentCore** agents accept either a JWT from an OIDC issuer like this one or IAM SigV4 request
  signing. `infer` sends bearer tokens only, so configure such an agent for JWT inbound auth.

## See Also

- [A2A Agents Configuration, Authentication](../../docs/agents-configuration.md#authentication)
- [a2a-auth](../a2a-auth/), the same agent behind a static token and behind Keycloak
- [a2a-auth-gcp](../a2a-auth-gcp/) and [a2a-auth-entraid](../a2a-auth-entraid/)
- [Inbound authentication for Amazon Bedrock AgentCore](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/runtime-oauth.html)
