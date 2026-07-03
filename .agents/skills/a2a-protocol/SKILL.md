---
name: a2a-protocol
description: >
  Build, consume, debug, or reason about Agent2Agent (A2A) protocol agents -
  Agent Cards and discovery, the Task lifecycle, Messages/Parts/Artifacts, the
  three transport bindings (JSON-RPC 2.0, gRPC, HTTP+JSON), request/response vs
  streaming (SSE) vs push notifications, security schemes, webhook security, and
  A2A error codes. Use whenever the work touches A2A: writing or reviewing an
  agent server or client, designing an agent card, delegating tasks between
  agents, wiring the CLI's A2A tools, or answering "how does A2A do X". The
  source of truth is the schema in inference-gateway/schemas - reference it,
  never hand-copy it, and always pin the spec version you target (v0.3.0 vs
  v1.0 differ in breaking ways).
license: Apache-2.0
---

# Agent2Agent (A2A) Protocol

A2A is an open protocol for **agent-to-agent** interoperability: one agent (the
**client**) discovers and delegates work to another (the **remote agent** /
server) over HTTP, without either exposing its internal tools, memory, or
prompts. It is **complementary to MCP** - MCP connects an agent to tools,
resources, and context; A2A connects an agent to *other agents*. It is built on
existing standards (HTTP, SSE, JSON-RPC 2.0). Governance: created by Google
(April 2025), donated to the **Linux Foundation** (June 2025); the canonical repo
moved `google-a2a/A2A` → **`a2aproject/A2A`**, Apache-2.0.

## Source of truth - reference it, don't copy it

The canonical A2A types for this ecosystem live in
[`inference-gateway/schemas`](https://github.com/inference-gateway/schemas). The
`a2a/a2a.proto` file is synced from upstream `a2aproject/A2A` and **generates**
`a2a/a2a-schema.{json,yaml}`. Never paste schema definitions into code or docs -
**point at the schema and regenerate**, so you never drift from the source.

| Need | Where to look |
| --- | --- |
| Authoritative types (this ecosystem) | `schemas/a2a/a2a-schema.json` (gen'd) / `a2a/a2a.proto` (source) |
| The prose spec, per version | <https://a2a-protocol.org/latest/specification/> (pin: `/v0.3.0/`, `/v1.0/`) |
| The runtime types the CLI speaks | `github.com/inference-gateway/adk` (`adk/types`) |
| Curated agents, SDKs, tools | [`inference-gateway/awesome-a2a`](https://github.com/inference-gateway/awesome-a2a) |

## Version map - read this first

A2A's latest released spec is **v1.0.0** (prior lines: 0.3.0, 0.2.x, 0.1.0;
versions are `Major.Minor`). **v0.3.0 → v1.0 is breaking.** Much online guidance
still describes v0.x, so *always know which shape you're looking at*:

| Aspect | v0.x shape (e.g. ADK `adk@v0.7.4`, the CLI runtime) | v1.0 shape (e.g. the `schemas` proto) |
| --- | --- | --- |
| Task states | lowercase kebab: `submitted`, `working`, `input-required`, `canceled` | ProtoJSON `SCREAMING_SNAKE`: `TASK_STATE_SUBMITTED`, … |
| JSON-RPC methods | slash names: `message/send`, `tasks/get` | PascalCase = gRPC: `SendMessage`, `GetTask` |
| Transport advertising | `url` + `preferredTransport` + `additionalInterfaces` | one `supportedInterfaces[]` array (first entry preferred) |
| Part content | separate `TextPart` / `FilePart` / `DataPart` | one unified `Part` (a `oneof`) |
| Error codes | `-32001`…`-32007` | adds `-32008`, `-32009`; `-32007` renamed |

> **Do not mix shapes.** Pick a target version, read *that* version's schema, and
> use its field/method names. The well-known path also moved with versions:
> pre-v0.3.0 used `/.well-known/agent.json`; **v0.3.0+ uses
> `/.well-known/agent-card.json`** (RFC 8615). `protocolVersion` is a top-level
> `AgentCard` field in the `schemas` proto - if a doc claims otherwise, trust the
> schema you're compiling against.

## Mental model

- **Agent Card** - the agent's public manifest (identity, skills, transports,
  security). Discovery starts here.
- **Task** - the core unit of work. Server-created, stable `id`, a `status`
  (state + message), `artifacts` (outputs), `history`.
- **Context** (`contextId`) - server-generated id that groups many tasks (one per
  turn) and messages into one conversation. The client echoes it back to
  continue; the agent may use it to hold internal state/history across turns.
- **Message** - one turn (`role: user | agent`), one or more **Parts**; may cite
  prior work via `referenceTaskIds`.
- **Part** - `text`, `file` (bytes or uri + mimeType), or `data` (structured JSON).
- **Artifact** - a durable task output, itself a list of Parts.
- **Opaque agents** - agents expose *capabilities*, not internals; interact only
  through the protocol, never assume the remote's tools/prompts/memory.

## Agent Card & discovery

A2A defines **three discovery mechanisms**: (1) **Well-Known URI** - host the card
at `https://{domain}/.well-known/agent-card.json` (RFC 8615); (2) **Curated
Registries** - a catalog service clients query (see the ecosystem's `registry`);
(3) **Direct Configuration** - hardcoded/env/config for private discovery.

Card authoring:

- **Write real `skills`.** Each `AgentSkill` has `id`, `name`, `description`,
  `tags` (required) plus `examples`, `inputModes`, `outputModes` (optional) -
  this is how a client (or an LLM) decides whether to route to you. Vague skills
  get skipped.
- **Declare `capabilities` you truly support:** `streaming`, `pushNotifications`,
  `stateTransitionHistory`. Clients negotiate on these - don't advertise what you
  can't do.
- **Advertise transports honestly** - v1.0 lists one `supportedInterfaces[]`
  (each interface carries its own `url` + `protocolBinding` + `protocolVersion`);
  v0.x uses `preferredTransport` + `additionalInterfaces`. Every advertised
  interface must be **functionally equivalent**.
- **Never put secrets in the card.** Declare *how* to authenticate via
  `securitySchemes` + `security`; credentials come out-of-band. If the card
  itself carries sensitive data, protect the endpoint (auth/mTLS/network) and/or
  serve an **authenticated extended card** with the extra detail.
- **Signed cards:** `signatures` (JWS, RFC 7515) let clients verify authenticity.

## Transports - three equal bindings, one behavior

A2A defines three wire bindings; an agent **MUST implement at least one** and MAY
offer several. They are **functionally equivalent** - same operations, same
semantics. `protocolBinding` values: `JSONRPC`, `GRPC`, `HTTP+JSON` (JSON-RPC is
the default when a preference is unspecified).

| Operation | v0.x JSON-RPC | v1.0 / gRPC | HTTP+JSON (REST) |
| --- | --- | --- | --- |
| Send message | `message/send` | `SendMessage` | `POST /message:send` |
| Stream | `message/stream` | `SendStreamingMessage` | `POST /message:stream` |
| Get task | `tasks/get` | `GetTask` | `GET /tasks/{id}` |
| List tasks | `tasks/list` | `ListTasks` | `GET /tasks` |
| Cancel task | `tasks/cancel` | `CancelTask` | `POST /tasks/{id}:cancel` |
| Resubscribe | `tasks/resubscribe` | `SubscribeToTask` | `GET /tasks/{id}:subscribe` |
| Push config CRUD | `tasks/pushNotificationConfig/*` | `*TaskPushNotificationConfig` | `/tasks/{id}/pushNotificationConfigs` |
| Extended card | `agent/getAuthenticatedExtendedCard` | `GetExtendedAgentCard` | `GET /extendedAgentCard` |

> **In this repo:** the ADK client (`client.NewClient(url)`) speaks **JSON-RPC**
> at the v0.x shape - `GetAgentCard`, `SendTask` (→ `message/send`), `GetTask`
> (→ `tasks/get`) - while `schemas` stores the v1.0 proto/gRPC form. Same
> operations, different binding *and* different spec version; that's why names
> differ between the schema you read and the calls the runtime makes.

## Task lifecycle

A send returns **either** a `Message` (immediate, self-contained reply) **or** a
`Task` (tracked through a lifecycle). Nine `TaskState` values:

- **Non-terminal:** `submitted` → `working`.
- **Interrupts (resumable):** `input-required` (needs more input),
  `auth-required` (needs out-of-band auth). Continue by sending a new Message with
  the same `taskId` + `contextId`.
- **Terminal (immutable):** `completed`, `failed`, `canceled`, `rejected`. Plus
  `unknown` for indeterminate. A terminal task **cannot restart** - do follow-up
  work as a *new* task in the same `contextId`.

Guidance: one Task per unit of work; reuse `contextId` to relate turns; put
results in `artifacts` and a human-readable summary in `status.message`. (Casing
is version-dependent - see the version map; normalize `canceled`/`CANCELLED`.)

## Messaging & content

- `Message.role` is `user` (client→agent) or `agent` (agent→client).
- A `Part` is exactly one of `text`, `file` (small → base64 `bytes`; large → a
  `uri`; always set the media type), or `data` (structured JSON). v1.0 unifies
  these into one `Part` type; v0.x had separate `*Part` types.
- **Artifacts** are the durable outputs; stream them incrementally with
  `TaskArtifactUpdateEvent` (`append` / `lastChunk`) and reassemble client-side.
- Honor `defaultInputModes` / `defaultOutputModes` (MIME types), overridable per
  skill and per request (`acceptedOutputModes`).

## Interaction patterns - request/response, streaming, push

The spec's canonical patterns are three (note: **polling is a sub-mode of
request/response**, not a separate pattern):

- **Synchronous request/response** - `message/send`; optionally **poll**
  `tasks/get` on an interval for longer work. Simplest, always available; use
  **exponential backoff** (this is what the CLI's A2A tools do).
- **Streaming (SSE)** - `message/stream` / `tasks/resubscribe`. Requires
  `capabilities.streaming`. The server emits `TaskStatusUpdateEvent` (watch
  `final: true` for the terminal event) and `TaskArtifactUpdateEvent` (chunks).
  Best for real-time incremental output; resubscribe to resume a dropped stream.
- **Push notifications** - `tasks/pushNotificationConfig/set` a webhook, for
  long-running work (minutes→days) or disconnected clients (mobile, serverless).
  Requires `capabilities.pushNotifications`.

### Securing push webhooks (the sharp edge)

Webhooks turn your agent into an HTTP client pointed at a caller-supplied URL -
treat every field as hostile:

- **Authenticate the sender.** The server MUST authenticate to the webhook per
  `PushNotificationConfig.authentication` (Bearer/OAuth2, API key, HMAC, or mTLS).
  Recommended JWT pattern: server signs with its private key; the webhook fetches
  the public key from the server's **JWKS** endpoint and verifies the signature
  plus `iss`/`aud`/`iat`/`exp`/`jti`.
- **Prevent SSRF.** Never blindly POST to a client-supplied URL - **allowlist
  domains** and verify URL ownership before sending.
- **Prevent replay.** Include a timestamp + single-use id (`jti`/nonce); the
  webhook rejects stale or repeated events. Webhook targets must be HTTPS.

## Security

- **HTTPS + modern TLS (1.3+)** in production; agent URLs are absolute HTTPS;
  clients validate the remote cert against trusted CAs.
- Declare auth in `securitySchemes` - five OpenAPI-derived types: `apiKey`,
  `http` (Basic/Bearer), `oauth2` (flows: authorizationCode, clientCredentials,
  implicit, password), `openIdConnect`, `mutualTLS`.
- **Credentials ride in HTTP headers, never in the payload**, and are obtained
  out-of-band; the card says *how* to authenticate, never *what* the secret is.
- Enforce authorization per skill where it matters (`AgentSkill.security`).

## Error handling

Return standard JSON-RPC errors (`-32700` parse, `-32600` invalid request,
`-32601` method not found, `-32602` invalid params, `-32603` internal) plus the
A2A-specific codes:

| Code | Meaning |
| --- | --- |
| `-32001` | TaskNotFound |
| `-32002` | TaskNotCancelable |
| `-32003` | PushNotificationNotSupported |
| `-32004` | UnsupportedOperation |
| `-32005` | ContentTypeNotSupported |
| `-32006` | InvalidAgentResponse |
| `-32007` | ExtendedAgentCardNotConfigured (v0.x: *Authenticated*ExtendedCardNotConfigured) |
| `-32008` | ExtensionSupportRequired (v1.0+) |
| `-32009` | VersionNotSupported (v1.0+) |

Distinguish **protocol errors** (malformed request → JSON-RPC error) from **task
failure** (the work ran and failed → a Task in state `failed`, reason in
`status.message`). Clients must handle both.

## Versioning & interoperability

- Set/read `protocolVersion`; negotiate on `capabilities` - don't call
  `message/stream` unless the card advertises `streaming`.
- Add fields backward-compatibly; keep advertised transports equivalent.
- Validate with the official
  [A2A Inspector](https://github.com/a2aproject/a2a-inspector) (card + JSON-RPC
  surface) and the [A2A TCK](https://github.com/a2aproject/a2a-tck) conformance
  suite (gRPC / JSON-RPC / HTTP+JSON) before claiming compliance.

## Building & consuming in this ecosystem

- **Build an agent:** the **[ADK](https://github.com/inference-gateway/adk)** (Go,
  plus Rust/TypeScript flavors) or declare it in YAML with
  **[ADL](https://github.com/inference-gateway/adl)** +
  [`adl-cli`](https://github.com/inference-gateway/adl-cli) and scaffold.
- **Consume from the CLI:** the `A2A_QueryAgent` / `A2A_SubmitTask` /
  `A2A_QueryTask` tools (`internal/agent/tools/a2a_*.go`), gated by `a2a.enabled`
  in `.infer` config; register agents in `.infer/agents.yaml`.
- **Debug:** [`a2a-debugger`](https://github.com/inference-gateway/a2a-debugger)
  to inspect/replay traffic; `mock-agent` for canned responses in tests.
- **Proxy to an LLM:** the
  [Inference Gateway](https://github.com/inference-gateway/inference-gateway).
- **Official SDKs** (`a2aproject`): `a2a-python` (`pip install a2a-sdk`),
  `a2a-js` (`@a2a-js/sdk`), `a2a-go`, `a2a-java`, `a2a-dotnet`, `a2a-rs`.

## Checklist

Building a server:

- [ ] Card at `/.well-known/agent-card.json` with required fields + descriptive `skills`.
- [ ] `capabilities` flags match reality; advertised transports are equivalent.
- [ ] Tasks get stable `id` + `contextId`; terminal states stay terminal.
- [ ] `securitySchemes` declared; HTTPS/TLS enforced; no secrets in the card.
- [ ] Push webhooks: server authenticates, SSRF-allowlisted, replay-guarded; correct A2A error codes.

Building a client:

- [ ] Fetch + cache the card; negotiate on `capabilities`, don't assume.
- [ ] Pick request/response (poll with backoff) / streaming / push per task duration.
- [ ] Carry `contextId`/`taskId` to continue; handle `input-required` + `auth-required`.
- [ ] Handle both JSON-RPC errors and `failed` Tasks; pin the spec version you target.

---

*Grounded in the [A2A spec](https://a2a-protocol.org/latest/specification/) (the
[`a2aproject/A2A`](https://github.com/a2aproject/A2A) upstream, Linux Foundation)
and the source-of-truth schema in
[`inference-gateway/schemas`](https://github.com/inference-gateway/schemas)
(`a2a/a2a.proto`). Reference the schema for exact fields - this skill teaches the
model, not the bytes.*
