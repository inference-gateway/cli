# Kubernetes Example: k3d + Inference Gateway Operator + CLI

End-to-end deployment of the `infer` CLI on a local k3d cluster using the
Inference Gateway Operator, with channel-manager mode, A2A agents, and
OpenTelemetry telemetry.

## Prerequisites

- [k3d](https://k3d.io/) v5.x
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [helm](https://helm.sh/) v3.x
- [task](https://taskfile.dev/) (or `go-task`)
- [infer](https://github.com/inference-gateway/cli) binary in PATH

## Quick start

```bash
# 1. Create the k3d cluster with cert-manager
task cluster:create

# 2. Install the Inference Gateway Operator
task operator:install

# 3. Deploy the Gateway, Orchestrator, Agents, and Redis
task deploy

# 4. Wait for everything to be ready
kubectl wait --for=condition=Ready pods --all -n infer-system --timeout=300s
kubectl wait --for=condition=Ready pods --all -n infer-orchestrator --timeout=300s
kubectl wait --for=condition=Ready pods --all -n infer-agents --timeout=300s

# 5. Port-forward the Gateway
kubectl port-forward -n infer-system svc/inference-gateway 8080:8080 &

# 6. Verify the Gateway is reachable
infer status

# 7. List channels (channel-manager mode)
infer channels list

# 8. View telemetry traces
infer telemetry traces --list

# 9. Clean up
task cleanup
```

## Architecture

```text
k3d cluster
├── cert-manager (TLS certificates)
├── Inference Gateway Operator
│   ├── Gateway (infer-system namespace)
│   │   └── Kubernetes Gateway API ingress
│   ├── Orchestrator (infer-orchestrator namespace)
│   │   ├── channel-manager mode
│   │   └── Redis (pub/sub message queue)
│   ├── A2A Agents (infer-agents namespace)
│   │   └── mock-agent
│   └── otel-collector (infer-system namespace)
│       └── OpenTelemetry traces/metrics
└── infer CLI (external)
    ├── infer status → Gateway health
    ├── infer channels → channel-manager commands
    └── infer telemetry traces → distributed traces
```

## CLI commands

### `infer status`

Verifies the Gateway is reachable and healthy:

```bash
infer status
```

Expected output:

```text
Gateway: http://localhost:8080
Status: healthy
```

### `infer channels`

Manages the channel-manager mode:

```bash
# List all channels
infer channels list

# Create a new channel
infer channels create --name my-channel --type telegram

# Delete a channel
infer channels delete my-channel
```

### `infer telemetry traces`

Views distributed traces from the orchestrator and agents:

```bash
# List all trace sessions
infer telemetry traces --list

# View a specific trace
infer telemetry traces <session-id>

# View traces in JSON format
infer telemetry traces --format json
```

## Telemetry

The operator's otel-collector configuration handles traces and metrics.
The CLI exports its own telemetry via OTLP push to the collector, and the
collector fans external spans (from the orchestrator and agents) back to
the CLI's local OTLP receiver for `infer telemetry traces` to display.

See the [operator's otel-collector configuration](https://github.com/inference-gateway/operator)
for the full collector setup.

## Manifests

All manifests are self-contained in this directory:

| File | Purpose |
| ------ | --------- |
| `namespace.yaml` | Namespaces for system, orchestrator, and agents |
| `redis.yaml` | Redis deployment for channel-manager pub/sub |
| `gateway.yaml` | Inference Gateway deployment and service |
| `orchestrator.yaml` | Orchestrator (channel-manager mode) deployment |
| `agents.yaml` | A2A mock-agent deployment |
| `otel-collector.yaml` | OpenTelemetry Collector for traces/metrics |
| `ingress.yaml` | Kubernetes Gateway API ingress |
| `Taskfile.yml` | Automation tasks |

## Cleanup

```bash
task cleanup
```

This deletes the k3d cluster and all resources.

## See also

- [Inference Gateway Operator](https://github.com/inference-gateway/operator)
- [Inference Gateway CLI](https://github.com/inference-gateway/cli)
- [A2A Protocol](https://github.com/inference-gateway/schemas)
- [OpenTelemetry Collector](https://opentelemetry.io/docs/collector/)
