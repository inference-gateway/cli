# Kubernetes Example: k3d + Inference Gateway Operator + CLI

End-to-end deployment on a local [k3d](https://k3d.io/) cluster using the
[Inference Gateway Operator](https://github.com/inference-gateway/operator):
a Gateway, an Orchestrator (the `infer` CLI in channels-manager mode), an A2A
mock agent, and an OpenTelemetry collector - all declared as operator CRDs
(`core.inference-gateway.com/v1alpha1`).

It also ships a **key-free chat + tracing demo**: a mock gateway (no LLM
provider needed), an `infer chat` container you exec into, and a Jaeger UI - so
you can watch a request flow `infer -> gateway` and `infer -> a2a` end to end.
Jump to [Chat and traces without any API keys](#chat-and-traces-without-any-api-keys).

## Prerequisites

- [k3d](https://k3d.io/) v5.x
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [task](https://taskfile.dev/) (or `go-task`)
- [infer](https://github.com/inference-gateway/cli) binary in PATH

## Quick start

```bash
# 1. Create the k3d cluster
task cluster:create

# 2. Install the Inference Gateway Operator (CRDs + controller)
task operator:install

# 3. Deploy the Gateway, Orchestrator, Agent, and otel-collector
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
    ├── Orchestrator "orchestrator"      (CRD → infer CLI, channels-manager mode)
    ├── Agent "mock-agent"               (CRD → A2A mock agent)
    ├── otel-collector                   (plain Deployment, OTLP :4317/:4318)
    ├── jaeger                            (plain Deployment, UI :16686)
    ├── mock-gateway                     (plain Deployment, Service :8080 - canned /v1/models + chat)
    └── infer-chat                       (plain Deployment - infer CLI you exec into)
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

The `infer chat` container talks to the **mock gateway** (canned OpenAI-compatible
responses), so no LLM provider credentials are needed. A scripted scenario makes
the chat call the `mock-agent` over A2A, producing a trace that spans
`infer -> gateway` and `infer -> a2a`.

```bash
# 1. Build the mock-gateway image and import it into the cluster
task mockgateway:image

# 2. Deploy everything (mock gateway, infer-chat, Jaeger included)
task deploy
kubectl wait --for=condition=Ready pods --all -n infer --timeout=300s

# 3. Chat inside the cluster, then type:  ask the mock agent hello
task chat

# 4. See infer's own span tree (session → chat → A2A_SubmitTask)
task traces

# 5. See the full distributed trace, including the mock-agent's server spans
task jaeger:ui   # then open http://localhost:16686 and pick service "infer-chat"
```

`infer traces` reads local per-session files inside the chat pod, so it shows
**infer's** view: the `chat openai/gpt-4o` client span is the `infer -> gateway`
hop and `execute_tool A2A_SubmitTask` is the `infer -> a2a` hop. The mock gateway
is a canned server and **emits no span of its own** - the `chat` client span *is*
the gateway hop here. Jaeger adds the a2a agent's server-side spans (it has
telemetry enabled), joined to infer's client span via `traceparent` propagation.
To also see a real gateway-side span, point `infer-chat`'s `INFER_GATEWAY_URL` at
the operator Gateway (`http://inference-gateway:8080`) with a provider configured.

## Notes

- The `infer` namespace carries the `inference-gateway.com/managed: "true"`
  label - the operator only reconciles CRs in namespaces labeled this way.
- The `telegram-bot-credentials` Secret in `orchestrator.yaml` holds a dummy
  token: the channels-manager needs at least one enabled channel to boot, so
  Telegram is enabled but its poller just logs an error and stops while the
  daemon (scheduler) keeps running. Put a real bot token in the Secret to use
  Telegram for real.
- No LLM provider credentials are needed for `infer status` - it only checks
  Gateway health. To chat through the Gateway, configure a provider on the
  `Gateway` CR (see the operator's
  [samples](https://github.com/inference-gateway/operator/tree/main/config/samples)).
- View collector output (traces/metrics arriving):
  `kubectl logs -n infer deploy/otel-collector -f`
