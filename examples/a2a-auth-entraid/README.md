# A2A agent behind Microsoft Entra ID

The same setup as [a2a-auth](../a2a-auth/), with Microsoft Entra ID as the issuer instead of Keycloak. Read that
example first: it explains how the agent verifies tokens and how `agents.yaml` carries credentials, none of which
changes here.

```text
infer ──► mock-agent  (A2A_AUTH_ISSUER_URL=https://login.microsoftonline.com/<tenant>/v2.0)
  │
  ╰──► login.microsoftonline.com  client-credentials grant, scope api://<API_APP_ID>/.default
```

Two app registrations are involved: one represents the agent (the API the tokens are issued _for_) and one the
caller that requests them. `infer` runs the client-credentials grant itself from the `oidc` block, reading the token
endpoint from the agent card and the client secret from `AZURE_CLIENT_SECRET`. Entra requires the
`api://<API_APP_ID>/.default` scope on that grant and the card declares none, which is what `scopes` is for.

The model is a [tokenless](https://github.com/inference-gateway/tokenless) mock scripted in
[`scenarios.yaml`](scenarios.yaml), so the example needs no LLM key. You need Docker, [Task](https://taskfile.dev)
and the [Azure CLI](https://learn.microsoft.com/cli/azure/) logged in to the tenant.

## Setup

```bash
TENANT_ID=$(az account show --query tenantId -o tsv)

# The API. Request v2 tokens, or Entra issues v1 tokens with another issuer.
API_APP_ID=$(az ad app create --display-name infer-a2a-agent --query appId -o tsv)
az ad app update --id "$API_APP_ID" --identifier-uris "api://$API_APP_ID" \
  --set api.requestedAccessTokenVersion=2
az ad sp create --id "$API_APP_ID"

# The caller: a confidential client with a secret.
CLIENT_APP_ID=$(az ad app create --display-name infer-a2a-client --query appId -o tsv)
az ad sp create --id "$CLIENT_APP_ID"
CLIENT_SECRET=$(az ad app credential reset --id "$CLIENT_APP_ID" --query password -o tsv)

cat > .env <<ENV
AZURE_TENANT_ID=$TENANT_ID
AZURE_API_APP_ID=$API_APP_ID
AZURE_CLIENT_APP_ID=$CLIENT_APP_ID
AZURE_CLIENT_SECRET=$CLIENT_SECRET
ENV
sed -i.bak -e "s/YOUR_TENANT_ID/$TENANT_ID/" -e "s/YOUR_API_APP_ID/$API_APP_ID/" \
  -e "s/YOUR_CLIENT_APP_ID/$CLIENT_APP_ID/" .infer/agents.yaml && rm .infer/agents.yaml.bak
```

## Run it

```bash
task up
task all
task down
```

| Task | What happens |
| --- | --- |
| `task up` | Starts the agent on `localhost:8093`, trusting the tenant's `/v2.0` issuer for the API's client ID |
| `task delegate` | `infer` fetches a token with the client-credentials grant and the model delegates. The task completes |
| `task wrong-token` | A wrong client secret. Entra refuses to issue a token and the model sees an authentication failure |
| `task token` | Prints a token for the API as _your_ signed-in account into `.token`, for `curl` or a `token_file` variant |
| `task all` | Runs `delegate` and `wrong-token` |
| `task chat` | Opens `infer chat` with the mock model. Try "ask the entra agent to echo hello" |
| `task down` | Stops the agent |

## Configuration notes

- [`.infer/agents.yaml`](.infer/agents.yaml) holds the `oidc` block. `issuer_url` pins the card to the tenant's
  issuer, `scopes` adds `api://<API_APP_ID>/.default`, and the secret is only ever named by its variable.
- To go secretless, replace the block in your `~/.infer/agents.yaml` with the token the Azure CLI or a managed
  identity mints:

  ```yaml
  auth:
    token_command: [az, account, get-access-token, --resource, api://<API_APP_ID>, --query, accessToken, -o, tsv]
  ```

  On Azure, with a managed identity, point `token_command` at a script that reads the IMDS token endpoint, or
  write the token to a file and use `token_file`.
- **v1 tokens.** If `requestedAccessTokenVersion` stays unset, Entra issues v1 tokens with
  `iss: https://sts.windows.net/<tenant>/`, which the agent rejects because it discovered the `/v2.0` issuer.
  Decode the token at [jwt.ms](https://jwt.ms) and check the `ver` claim.
- **`AADSTS500011`** means the API has no service principal in the tenant; the `az ad sp create` step creates it.
- **Every app in the tenant can request a token for your API** unless the API's service principal has "assignment
  required" turned on. Authentication proves the token came from your tenant for your API, not _which_ app asked.

## See Also

- [A2A Agents Configuration, Authentication](../../docs/agents-configuration.md#authentication)
- [a2a-auth](../a2a-auth/), the same agent behind a static token and behind Keycloak
- [a2a-auth-gcp](../a2a-auth-gcp/) and [a2a-auth-aws](../a2a-auth-aws/)
- [Agent2Agent authentication in Microsoft Foundry](https://learn.microsoft.com/en-us/azure/foundry/agents/concepts/agent-to-agent-authentication)
