# Session transfer: move a daemon session to a Kubernetes executor

Move a running `infer daemon` session from your laptop to a Kubernetes sandbox and pick it up from any
client. The worker process never moves: the daemon checkpoints the conversation and the project's git
state, a pod running the same `infer` image as a daemon restores them, ownership moves with a lease, and
every client keeps talking to its own daemon, which relays the thread to the pod. Closing the laptop does
not stop the pod.

The cluster is a local [k3d](https://k3d.io/) cluster running the
[agent-sandbox](https://github.com/kubernetes-sigs/agent-sandbox) controller, which gives every session
a `Sandbox`: a stable pod with two persistent volumes (the workspace and the conversation store) that the
pod daemon suspends when idle and the next transfer resumes. Two executors are configured: `k8s-dev`
authenticates the pod with mTLS from the daemon's own CA, `k8s-spiffe` with SPIFFE SVIDs issued by SPIRE
inside the cluster and rotated by the SPIRE agents.

Everything runs against the built-in mock model, so no API key is needed. See [infer daemon](../../docs/daemon.md#executors-and-transfers)
for the executor model and the [Daemon Binding Protocol](../../docs/browser-extension-protocol.md#executors)
for the wire frames.

## Prerequisites

- [k3d](https://k3d.io/) v5, [kubectl](https://kubernetes.io/docs/tasks/tools/), [helm](https://helm.sh/) (SPIFFE only)
- [docker](https://docs.docker.com/get-docker/) (k3d runs on it, `task image:import` builds with it)
- [task](https://taskfile.dev/) and Go 1.26 (the image build and, for SPIFFE, the `spire-agent` build)
- `task build` at the repository root: the tasks run that `infer` binary (`INFER=<path>` overrides it)

## Layout

| File | Purpose |
| --- | --- |
| `daemon.yaml` | The laptop daemon's config: the binding and the `k8s-dev` (CA) and `k8s-spiffe` (SPIFFE) executors |
| `k8s/namespace.yaml` | The `infer-executors` namespace, the `infer-executor` ServiceAccount, and the Role a pod uses to suspend its Sandbox |
| `k8s/spire-values.yaml` | Helm values for SPIRE: trust domain `infer.local`, join-token node attestation, the default SPIFFE ID per ServiceAccount |
| `spire/agent.conf.tmpl` | The laptop `spire-agent` config, filled in by `task spire:agent` |
| `Taskfile.yml` | Every step below |
| `.home/` | Created by the tasks: a throwaway `HOME` with the daemon config, the demo project and the SPIRE agent state |

The tasks run the daemon under `.home/` as `HOME`, so your own `~/.infer` stays untouched.

## Quick start: the daemon CA executor

```bash
# 1. The cluster, the agent-sandbox controller and the executor namespace
task cluster:create

# 2. Build the infer image from this checkout and import it into the cluster
task image:import

# 3. In its own shell: the laptop daemon (binding on 127.0.0.1:52789)
task daemon:start

# 4. In another shell: create a conversation, move it, continue it on the pod
task demo
```

`task demo` prints the executors first, then the transfer's stages, the Sandbox and its pod, and the
reply of a turn that ran on the pod:

```text
NAME        KIND        REACHABLE  DETAILS
laptop      local       true       storage=jsonl version=v0.238.0
k8s-dev     kubernetes  true       auth=ca image=ghcr.io/inference-gateway/cli:local kubernetes=v1.31.5+k3s1 namespace=infer-executors sandboxes=0
k8s-spiffe  kubernetes  true       auth=spiffe ...
provisioning
checkpointing
restoring
ready
session now lives on k8s-dev in /workspace/project/<conversation id>
NAME                                         READY   REASON              AGE
sandbox.agents.x-k8s.io/infer-7eb7f642fa3d   True    DependenciesReady   8s
Hello! How can I help?
```

What each step proved:

- `provisioning` created the `Sandbox` from the executor entry, a Secret with the pod's server certificate,
  its client certificate and a binding token issued by the laptop daemon's CA (`.home/.infer/daemon/pki/`),
  waited for `Ready` and opened a port-forward to the pod's binding.
- `checkpointing` stopped the laptop worker, exported the conversation through a worker in the project and
  packed the project's `HEAD` plus its uncommitted and untracked changes.
- `restoring` streamed the checkpoint to the pod daemon over mTLS, which cloned the bundle into
  `/workspace/project/<conversation id>`, imported the conversation through a worker there, claimed the
  lease, and answered `resume_conversation` with a snapshot the laptop checked against the checkpoint.
- `infer daemon run` sent a turn to the laptop daemon, which relayed it to the pod. The reply came from a
  worker running in the pod.

Look around:

```bash
# The conversation and the workspace on the pod
POD=$(kubectl -n infer-executors get pods -o jsonpath='{.items[0].metadata.name}')
kubectl -n infer-executors exec $POD -- sh -c 'ls /workspace/project/*/ && ls /home/infer/.infer/projects/*/conversations'

# A second transfer of the same session is refused: k8s-dev owns it now
HOME=$PWD/.home ../../infer daemon transfer <conversation id> --to k8s-spiffe --project-dir $PWD/.home/project

# Stop the laptop daemon (Ctrl+C in its shell), start it again, and the session is still on the pod
task daemon:start
HOME=$PWD/.home ../../infer daemon run <conversation id> "and again" --project-dir $PWD/.home/project
```

After `idle_ttl` (30 minutes in `daemon.yaml`) with no client and no run, the pod daemon suspends its own
Sandbox: `kubectl -n infer-executors get sandboxes` shows `Ready False` with reason `SandboxSuspended`, the
pod is gone and the two volumes stay. The next `infer daemon run` or transfer sets the Sandbox back to
`Running`, waits for the new pod and reattaches the volumes.

## SPIFFE: SVIDs from SPIRE instead of the daemon CA

The `k8s-spiffe` executor authenticates both ends with SPIFFE X.509 SVIDs. The pod gets its SVID
(`spiffe://infer.local/ns/infer-executors/sa/infer-executor`) from the SPIRE agent on its node through the
SPIFFE CSI driver, and the laptop daemon gets its own
(`spiffe://infer.local/executor/laptop/daemon`) from a `spire-agent` running on the laptop, attested to the
in-cluster SPIRE server with a join token. SPIRE rotates both.

```bash
# 1. SPIRE server, agents and CSI driver in the cluster (two helm releases in spire-mgmt)
task spire:install

# 2. In its own shell: build spire-agent for this machine (SPIRE publishes Linux binaries only),
#    register the laptop daemon, port-forward the SPIRE server and run the agent
task spire:agent

# 3. Restart the laptop daemon with the agent's Workload API socket
task daemon:start:spiffe

# 4. Move a fresh conversation to the SPIFFE executor
task demo TARGET=k8s-spiffe
```

`task spire:agent` creates a registration entry whose selector is your unix uid, so any process you run,
the daemon included, may fetch the laptop's SVID. The agent's Workload API socket is
`/tmp/infer-spire-agent.sock`, because macOS caps unix socket paths at 104 bytes and a path under `.home/`
easily exceeds that. The pod daemon trusts any member of `infer.local`, and
the laptop daemon trusts only the pod's SPIFFE ID, both set in `daemon.yaml`.

## Switching the pod to a real model

The image sets `INFER_GATEWAY_URL=http://inference-gateway:8080`, so a pod reaches a gateway Service of
that name in its namespace. Deploy one (the [kubernetes example](../kubernetes/) uses the operator for
this) with your provider keys in a Secret, drop `INFER_GATEWAY_MOCK` and set `INFER_AGENT_MODEL` in the
executor's `env`. Provider and MCP credentials the laptop's workers use can travel with the session instead:
list them under the executor's `forward_env`, and a transfer seals them to the pod's certificate for its
workers. They never land in a Kubernetes object.

## Cleanup

```bash
task cleanup
```

## Troubleshooting

| Symptom | Fix |
| --- | --- |
| `k8s-dev reachable false` with a CRD error | `task cluster:create` installs agent-sandbox, check `kubectl get crd sandboxes.agents.x-k8s.io` |
| `provisioning` hangs, pod `ImagePullBackOff` | Run `task image:import` after every rebuild |
| `provisioning` fails with `did not become Ready` | Check `kubectl -n infer-executors describe sandbox` and the pod's logs |
| `session ... is owned by executor` | The session already moved. Continue it with `infer daemon run`, or `stop_thread` it to delete the Sandbox |
| `restoring` fails with `does not apply cleanly` | The project changed after the transfer started. Retry |
| `checkpointing` fails with `not a git checkout with a commit` | A transfer bundles `HEAD`, so the project needs at least one commit |
| The relay drops with `RUN_ERROR` after the laptop slept | The port-forward died. The next `infer daemon run` opens a new one |
| SPIFFE: `fetching an SVID from the SPIRE Workload API` | Keep `task spire:agent` running and start the daemon with `task daemon:start:spiffe` |
| SPIFFE: the pod stays `ContainerCreating` | The CSI driver and SPIRE agent must run on the node: `kubectl get pods -n spire-system` |

Two things to know. The lease a relaying daemon keeps for a remote owner lapses after `lease_ttl` (the
executor's `idle_ttl` plus five minutes by default): a laptop that cannot reach the pod after that may
reclaim the session locally, with the conversation as of the last checkpoint. And a panel request such as
`list_conversations` from another client in the same project dir may be routed to the relay and answer from
the pod's store.
