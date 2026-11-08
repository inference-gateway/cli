# compute

**What** - the compute bounded context: what sessions and models run on, written down once and apart from any daemon
that uses it. Kubernetes clusters and standalone machines that workspaces run on, and GPUs rented by the hour to serve
a model.
**Why** - a cluster exists whether or not a daemon uses it. Keeping it inside each workspace repeated its connection per
workspace and gave every workspace its own Gateway. Capacity has its own rules: a cluster's namespace is shared by its
workspaces, one Gateway runs per cluster, and GPU spend has a cap.
**How** - `domain/` holds the GPU model and its rules (hourly price, spending cap, uptime and spend so far).
`infrastructure/kubernetes` connects to a cluster, checks it runs the operator and reports its status.
`infrastructure/runpod` rents GPUs from RunPod and translates its API into the domain, so RunPod's JSON never leaves it.

## How it plugs in

- Clusters, machines and GPU settings live in `~/.infer/compute.yaml`, read from home only (`config/compute.go`). A
  workspace in `daemon.yaml` names its compute with `compute`, and loading fills it with the compute's kind and
  connection.
- Placement stays in the agent context: the `Workspace` port, teleport and leases. The Kubernetes workspace adapter
  connects through `infrastructure/kubernetes`, gives every workspace on one cluster that cluster's Gateway, and labels
  each session's Orchestrator with its cluster so `infer compute clusters status` can count them.
- `infer compute clusters` and `infer compute machines` edit `compute.yaml` as written, so `${VAR}` references survive.
- `infer compute gpus` drives `infrastructure/runpod`. Its API key is `RUNPOD_API_KEY` in the environment or
  `~/.infer/auth.yaml`.

## Related

- [Compute](../../docs/daemon.md#compute)
- [Commands Reference](../../docs/commands-reference.md#infer-compute)
