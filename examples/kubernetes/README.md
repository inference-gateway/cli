# Kubernetes Example: k3d + Inference Gateway Operator + CLI

End-to-end deployment on a local [k3d](https://k3d.io/) cluster using the
[Inference Gateway Operator](https://github.com/inference-gateway/operator):
a Gateway, an Orchestrator (the `infer` CLI in daemon mode), an A2A
mock agent, and an OpenTelemetry collector - all declared as operator CRDs
(`core.inference-gateway.com/v1alpha1`).

It also ships a **key-free chat + tracing demo**: a mock gateway (no LLM
provider needed), an `infer chat` container you exec into, and a Jaeger UI - so
you can watch a request flow `infer -> gateway` and `infer -> a2a` end to end.
Jump to [Chat and traces without any API keys](#chat-and-traces-without-any-api-keys).

## Prerequisites

- [k3d](https://k3d.io/) v5.x (the cluster pins `rancher/k3s:v1.36.2-k3s1`, bump `K3S_VERSION` in `Taskfile.yml` to change it)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [task](https://taskfile.dev/) (or `go-task`)
- [docker](https://docs.docker.com/get-docker/) (k3d needs it; `task mockgateway:image` builds with it)
- [infer](https://github.com/inference-gateway/cli) binary in PATH (only for step 6 -
  the chat demo uses the in-cluster binary)

## Quick start

```bash
# 1. Create the k3d cluster
task cluster:create

# 2. Install the Inference Gateway Operator (CRDs + controller)
task operator:install

# 3. Deploy everything (Gateway, Orchestrator, Agent, otel-collector, Jaeger,
#    mock gateway). Builds and imports the mock-gateway:local image first.
task deploy

# 4. Wait for the operator to reconcile everything
kubectl wait --for=condition=Ready pods --all -n infer --timeout=300s
kubectl get gateways,agents,orchestrators -n infer

# 5. Port-forward the Gateway
kubectl port-forward -n infer svc/inference-gateway 8080:8080 &

# 6. Verify the Gateway is reachable
INFER_GATEWAY_URL=http://localhost:8080 infer status

# 7. Clean up
task cleanup
```

## Architecture

```text
k3d cluster
├── inference-gateway-system namespace
│   └── Inference Gateway Operator (reconciles the CRs below)
└── infer namespace
    ├── Gateway "inference-gateway"      (CRD → Deployment + Service :8080)
    ├── Orchestrator "orchestrator"      (CRD → infer CLI, daemon mode)
    ├── Agent "mock-agent"               (CRD → A2A mock agent)
    ├── otel-collector                   (plain Deployment, OTLP :4317/:4318)
    ├── jaeger                           (plain Deployment, UI :16686)
    └── mock-gateway                     (plain Deployment, Service :8080 - canned /v1/models + chat)
```

The Gateway, Orchestrator, and Agent are custom resources; the operator owns
their Deployments and Services. Telemetry is pushed via OTLP to the collector,
which logs it (`debug` exporter). To see a trace, send the mock agent an A2A
message and watch the collector logs:

```bash
kubectl port-forward -n infer svc/mock-agent 8081:8080 &
curl -s -X POST http://localhost:8081/a2a -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":"1","method":"message/send","params":{"message":{"role":"user","parts":[{"kind":"text","text":"hello"}],"messageId":"m1","kind":"message"}}}'
kubectl logs -n infer deploy/otel-collector | grep Traces
```

## Chat and traces without any API keys

`task chat` execs an interactive `infer chat` into the **Orchestrator** pod,
which talks to the operator-managed **Gateway**, whose
OpenAI provider is pointed at the in-cluster **mock gateway** (canned
OpenAI-compatible responses), so no LLM provider credentials are needed. A
scripted scenario makes the chat call the `mock-agent` over A2A, producing one
distributed trace covering `infer -> gateway -> mock` and `infer -> a2a`.

This assumes the cluster from [Quick start](#quick-start) steps 1-2 is already
up - `task mockgateway:image` imports into the `infer-demo` cluster and fails if
it does not exist:

```bash
task cluster:create
task operator:install
```

```bash
# 1. Build the mock-gateway image and import it into the cluster
task mockgateway:image

# 2. Deploy everything (mock gateway and Jaeger included)
task deploy
kubectl wait --for=condition=Ready pods --all -n infer --timeout=300s

# 3. Chat inside the Orchestrator pod
task chat
```

At the chat prompt type this **exact** phrase - it is what the mock gateway's
`a2a` scenario matches, and it is what makes the chat call the agent over A2A:

```text
ask the mock agent hello
```

Any other prompt (`hello`, for instance) falls through to the mock gateway's
`Done.` fallback, which produces no tool call and therefore no A2A span.

Then, in the chat, run `/traces` (or `task traces` from another shell) to get
the whole distributed tree:

```text
session (in progress)                                          118ms
├── chat openai/gpt-4o                                    4ms
│   ╰── POST /v1/chat/completions [inference-gateway]            3ms
│       ╰── POST /proxy/:provider/*path [inference-gateway]      1ms
├── execute_tool A2A_SubmitTask call_0_0                       691µs
│   ╰── a2a.request [mock-agent] call_0_0                      173µs
│       ╰── task.process [mock-agent]                          453ms
│           ├── tool.read [mock-agent] call_0_0                101ms
│           ├── tool.search [mock-agent] call_0_0              150ms
│           ╰── tool.fetch [mock-agent] call_0_0               201ms
╰── chat openai/gpt-4o                                    6ms
    ╰── POST /v1/chat/completions [inference-gateway]            4ms
        ╰── POST /proxy/:provider/*path [inference-gateway]      1ms
```

`infer traces` renders a local per-session file, which normally holds only
infer's own spans. The gateway's and agent's spans get in because the CLI runs
an **OTLP receiver** (`INFER_TELEMETRY_RECEIVER_ADDRESS=0.0.0.0:4318` in the
Orchestrator's `spec.env`, exposed by the `orchestrator` Service) and the
collector fans traces out to it
(`otlphttp/infer` exporter) alongside Jaeger. The receiver keeps only spans
carrying the active session's trace id.

The same trace is in the Jaeger UI if you prefer a timeline:

```bash
task jaeger:ui   # http://localhost:16686, service "orchestrator"
```

The Orchestrator runs the released `ghcr.io/inference-gateway/cli:latest`. Two
things need a CLI new enough: the `[inference-gateway]` spans (the gateway SDK
client must send W3C `traceparent`), and a readable TUI (the operator sets
`INFER_LOGGING_STDOUT=true` for the daemon, and `infer chat`
must ignore it - on an older image, prefix the exec with
`env INFER_LOGGING_STDOUT=false`). Only one process per pod can hold the
receiver port, so an `infer headless` subprocess spawned by a channel while you
are chatting will not get the remote spans.

Two things to know: the `session` root span is exported when the session ends,
so it shows as `(in progress)` while the chat is open; and a headless
`infer headless` run exits in ~2s, before the collector flushes, so the remote
spans miss it - use `infer chat` for the nested view.

Those `tool.*` spans are real: the scenario's `task_description` is
`simulate 3 tool calls`, which the mock agent routes through its real
instrumented tool path (see its
[simulating tool calls](https://github.com/inference-gateway/mock-agent/blob/main/docs/simulating-tool-calls.md)
docs). It needs `mock-agent` >= 0.4.0; earlier images emit only `a2a.request`.
The agent then reports the task as `TASK_STATE_CANCELLED` (`max streaming
iterations reached`) - the spans are emitted either way, which is all this demo
is about.

The Orchestrator points at the **operator-managed Gateway**
(`http://inference-gateway:8080`), whose OpenAI provider is pointed at the mock
gateway via `OPENAI_API_URL` - so the gateway does real work (and emits real
spans) without any provider credentials. The trace joins up because infer sends
W3C `traceparent` on both hops: gateway calls and A2A calls.

The mock gateway normally impersonates a *gateway*, so it advertises
provider-prefixed model ids (`openai/gpt-4o`). Here it sits *behind* the real
Gateway as the OpenAI provider upstream, and the Gateway adds the provider
prefix itself - which would give `openai/openai/gpt-4o`. It is started with
`-model gpt-4o` so it advertises the bare id a real OpenAI endpoint would, and
the model is plain `openai/gpt-4o`.

The mock gateway itself emits no spans; the last hop
(`POST /proxy/:provider/*path` → mock) is visible as the gateway's client span.

## Notes

- The `infer` namespace carries the `inference-gateway.com/managed: "true"`
  label - the operator only reconciles CRs in namespaces labeled this way.
- The `telegram-bot-credentials` Secret in `orchestrator.yaml` holds a dummy
  token: the daemon with channels enabled needs at least one enabled channel to boot, so
  Telegram is enabled but its poller just logs an error and stops while the
  daemon (scheduler) keeps running. Put a real bot token in the Secret to use
  Telegram for real.
- No LLM provider credentials are needed for `infer status` - it only checks
  Gateway health. To chat through the Gateway, configure a provider on the
  `Gateway` CR (see the operator's
  [samples](https://github.com/inference-gateway/operator/tree/main/config/samples)).
- View collector output (traces/metrics arriving):
  `kubectl logs -n infer deploy/otel-collector -f`
