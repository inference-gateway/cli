# Session transfer: move a daemon session to a Kubernetes environment

Move a running `infer daemon` session from your laptop to a Kubernetes sandbox and pick it up from any
client. The worker process never moves: the daemon checkpoints the conversation and the project's git
state, a pod running the same `infer` image as a daemon restores them, ownership moves with a lease, and
every client keeps talking to its own daemon, which relays the thread to the pod. Closing the laptop does
not stop the pod.

The cluster is a local [k3d](https://k3d.io/) cluster on k3s v1.37.1, declared in `Cluster.yaml` and created by
[ctlptl](https://github.com/tilt-dev/ctlptl), running the
[agent-sandbox](https://github.com/kubernetes-sigs/agent-sandbox) controller, which gives every session
a `Sandbox`: a stable pod with two persistent volumes (the workspace and the conversation store) that the
pod daemon suspends when idle and the next transfer resumes. Two environments are configured: `k8s-dev`
authenticates the pod with mTLS from the daemon's own CA, `k8s-spiffe` with SPIFFE SVIDs issued by SPIRE
inside the cluster and rotated by the SPIRE agents.

Everything runs against the built-in mock model, so no API key is needed. See [infer daemon](../../docs/daemon.md#environments-and-transfers)
for the environment model and the [Daemon Binding Protocol](../../docs/browser-extension-protocol.md#environments)
for the wire frames.

## Prerequisites

- [ctlptl](https://github.com/tilt-dev/ctlptl) and [k3d](https://k3d.io/) v5 (ctlptl drives k3d)
- [kubectl](https://kubernetes.io/docs/tasks/tools/), [helm](https://helm.sh/) (SPIFFE only)
- [docker](https://docs.docker.com/get-docker/) (k3d runs on it, `task image:import` builds with it)
- [task](https://taskfile.dev/) and Go 1.26 (the image build and, for SPIFFE, the `spire-agent` build)
- `task build` at the repository root: the tasks run that `infer` binary (`INFER=<path>` overrides it)

## Layout

| File | Purpose |
| --- | --- |
| `Cluster.yaml` | The ctlptl cluster: k3d `infer-environments` with one agent node, pinned to k3s v1.37.1 |
| `daemon.yaml` | The laptop daemon's config: the binding and the `k8s-dev` (CA), `k8s-spiffe` (SPIFFE) and `k8s-desktop` environments |
| `desktop/` | The `k8s-desktop` image: Xvfb, Chromium and ffmpeg on the infer image, the browse scenario and the artifact upload hook |
| `k8s/rustfs.yaml` | RustFS, the S3-compatible store the desktop's artifacts are uploaded to, with a 2Gi volume |
| `k8s/namespace.yaml` | The `infer-environments` namespace, the `infer-environment` ServiceAccount, and the Role a pod uses to suspend its Sandbox |
| `k8s/spire-values.yaml` | Helm values for SPIRE: trust domain `infer.local`, join-token node attestation, the default SPIFFE ID per ServiceAccount |
| `spire/agent.conf.tmpl` | The laptop `spire-agent` config, filled in by `task spire:agent` |
| `Taskfile.yml` | Every step below |
| `.home/` | Created by the tasks: a throwaway `HOME` with the daemon config, the demo project and the SPIRE agent state |

The tasks run the daemon under `.home/` as `HOME`, so your own `~/.infer` stays untouched.

## Quick start: the daemon CA environment

```bash
# 1. The cluster, the agent-sandbox controller and the environment namespace
task cluster:create

# 2. In its own shell: the laptop daemon (binding on 127.0.0.1:52789)
task daemon:start

# 3. In another shell: create a conversation, move it, continue it on the pod
task demo
```

The environments run the release candidate image `ghcr.io/inference-gateway/cli:0.239.0-rc.3`, which the pod
pulls from GHCR. To run this checkout on the pod instead, set both environments' `image` in `daemon.yaml` to
`ghcr.io/inference-gateway/cli:local` and run `task image:import` before `task daemon:start`.

`task demo` prints the environments first, then the transfer's stages, the Sandbox and its pod, and the
reply of a turn that ran on the pod:

```text
NAME         KIND        REACHABLE  DETAILS
laptop       local       true       storage=jsonl version=v0.239.0-rc.3
k8s-desktop  kubernetes  true       auth=ca image=infer-desktop:local ...
k8s-dev      kubernetes  true       auth=ca image=ghcr.io/inference-gateway/cli:0.239.0-rc.3 kubernetes=v1.37.1+k3s1 namespace=infer-environments sandboxes=0
k8s-spiffe   kubernetes  true       auth=spiffe ...
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

- `provisioning` created the `Sandbox` from the environment entry, a Secret with the pod's server certificate,
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
POD=$(kubectl -n infer-environments get pods -o jsonpath='{.items[0].metadata.name}')
kubectl -n infer-environments exec $POD -- sh -c 'ls /workspace/project/*/ && ls /home/infer/.infer/projects/*/conversations'

# A second transfer of the same session is refused: k8s-dev owns it now
HOME=$PWD/.home ../../infer daemon transfer <conversation id> --to k8s-spiffe --project-dir $PWD/.home/project

# Stop the laptop daemon (Ctrl+C in its shell), start it again, and the session is still on the pod
task daemon:start
HOME=$PWD/.home ../../infer daemon run <conversation id> "and again" --project-dir $PWD/.home/project
```

After `idle_ttl` (30 minutes in `daemon.yaml`) with no client and no run, the pod daemon suspends its own
Sandbox: `kubectl -n infer-environments get sandboxes` shows `Ready False` with reason `SandboxSuspended`, the
pod is gone and the two volumes stay. The next `infer daemon run` or transfer sets the Sandbox back to
`Running`, waits for the new pod and reattaches the volumes.

The pod only idles out when no run is open, so an agent you sent off keeps working with the laptop closed.
Once every agent on the pod is done, the suspension also starts the environment's `retention` (24 hours in
`daemon.yaml`): agent-sandbox then deletes the Sandbox with its volumes and its Secret, unless a run or a
transfer resumed it first. An agent that waits on an approval or a question, or whose last run failed, is not
done, and its Sandbox stays. `kubectl -n infer-environments get sandbox -o yaml` shows the countdown as
`spec.shutdownTime` and the verdict as the `inference-gateway.com/agents-done` annotation. To delete a
session's Sandbox now, whatever its state:

```bash
HOME=$PWD/.home ../../infer daemon stop <conversation id> --project-dir $PWD/.home/project
```

`stop` releases the lease too, so the session continues from the laptop's copy, as it was before the transfer.

## SPIFFE: SVIDs from SPIRE instead of the daemon CA

The `k8s-spiffe` environment authenticates both ends with SPIFFE X.509 SVIDs. The pod gets its SVID
(`spiffe://infer.local/ns/infer-environments/sa/infer-environment`) from the SPIRE agent on its node through the
SPIFFE CSI driver, and the laptop daemon gets its own
(`spiffe://infer.local/environment/laptop/daemon`) from a `spire-agent` running on the laptop, attested to the
in-cluster SPIRE server with a join token. SPIRE rotates both.

```bash
# 1. SPIRE server, agents and CSI driver in the cluster (two helm releases in spire-mgmt)
task spire:install

# 2. In its own shell: build spire-agent for this machine (SPIRE publishes Linux binaries only),
#    register the laptop daemon, port-forward the SPIRE server and run the agent
task spire:agent

# 3. Restart the laptop daemon with the agent's Workload API socket
task daemon:start:spiffe

# 4. Move a fresh conversation to the SPIFFE environment
task demo TARGET=k8s-spiffe
```

`task spire:agent` creates a registration entry whose selector is your unix uid, so any process you run,
the daemon included, may fetch the laptop's SVID. The agent's Workload API socket is
`/tmp/infer-spire-agent.sock`, because macOS caps unix socket paths at 104 bytes and a path under `.home/`
easily exceeds that. The pod daemon trusts any member of `infer.local`, and
the laptop daemon trusts only the pod's SPIFFE ID, both set in `daemon.yaml`.

## A computer of its own: the desktop environment

The `k8s-desktop` environment gives the session a Linux desktop inside its pod. The `desktop/` image adds Xvfb,
Chromium and ffmpeg to the release candidate image, and its entrypoint starts display `:99` with Chromium on it
before the pod daemon. `INFER_COMPUTER_USE_ENABLED` in the environment's `env` registers the `Computer` tool for the
pod's workers, so after the transfer the agent drives the pod's screen, never your laptop's.

```bash
# 1. Build the desktop image and import it into the cluster
task desktop:image

# 2. With the laptop daemon running: move a fresh conversation to the desktop and ask it to browse
task demo TARGET=k8s-desktop PROMPT="browse to example.com"

# 3. Save what the pod's screen shows to .home/desktop.png
task desktop:screenshot
```

The mock model plays `desktop/scenarios.yaml`: it starts a screen recording, then takes a screenshot, clicks
into the browser, presses `ctrl+l`, types the URL and presses `enter`, each a `Computer` call that runs in the
pod, and stops the recording. The reply is
`I opened https://example.com in the browser on my own desktop and recorded it.` and `.home/desktop.png`
shows Chromium on example.com. The recording is an artifact, see the next section. With a real model, drop
`INFER_GATEWAY_MOCK_SCENARIOS` and the agent picks the steps from its own
screenshots, so choose a vision model. The desktop lives as long as the pod: a suspended Sandbox comes back
with a fresh display and browser, and only the workspace and the conversation persist.

## Artifacts on durable storage: RustFS

What the agent produces on its computer outlives the pod. [RustFS](https://rustfs.com/), an open source
S3-compatible object store, runs in the cluster on its own volume, and when the agent is done with a run
everything it placed in `~/.infer/artifacts` or `~/.infer/projects/<project>/artifacts/<session id>` is
uploaded to the `infer-artifacts` bucket under the session id.

```bash
# 1. RustFS in the environment namespace (S3 API on rustfs:9000, console on rustfs:9001)
task artifacts:install

# 2. A conversation on the desktop: the agent browses and records its screen while it does
task demo TARGET=k8s-desktop PROMPT="browse to example.com"

# 3. The recording in RustFS, under the session id
task artifacts:list

# 4. Or browse the bucket at http://localhost:9001/rustfs/console/ (rustfsadmin / rustfsadmin)
task artifacts:console
```

```text
[2026-10-28 20:25:18 UTC]  59KiB STANDARD <conversation id>/20261028-202514.872.mp4
```

The listing runs in a pod of its own against RustFS, so it still shows the recording after the session's
Sandbox is suspended or deleted.

The upload is a `post_session` [command hook](../../docs/hooks.md) baked into the desktop image
(`desktop/hooks.yaml`). It runs `desktop/upload-artifacts` after every finished run, which reads the session
id from the hook's stdin and mirrors both artifact directories to `infer-artifacts/<session id>/` with the
MinIO client. The environment's `env` points recordings at
`~/.infer/artifacts`, allow-lists the hook with `INFER_TOOLS_BASH_ALLOW_APPEND`, and carries the endpoint,
bucket and credentials. A pod without `ARTIFACTS_ENDPOINT` skips the upload. The demo credentials sit in
`daemon.yaml` in plain text. Put real ones under the environment's `forward_env` instead, so they travel
sealed with the session and never land in the Sandbox spec.

## Switching the pod to a real model

The image sets `INFER_GATEWAY_URL=http://inference-gateway:8080`, so a pod reaches a gateway Service of
that name in its namespace. Deploy one (the [kubernetes example](../kubernetes/) uses the operator for
this) with your provider keys in a Secret, drop `INFER_GATEWAY_MOCK` and set `INFER_AGENT_MODEL` in the
environment's `env`. Provider and MCP credentials the laptop's workers use can travel with the session instead:
list them under the environment's `forward_env`, and a transfer seals them to the pod's certificate for its
workers. They never land in a Kubernetes object.

## Cleanup

```bash
task cleanup
```

## Troubleshooting

| Symptom | Fix |
| --- | --- |
| `k8s-dev reachable false` with a CRD error | `task cluster:create` installs agent-sandbox, check `kubectl get crd sandboxes.agents.x-k8s.io` |
| `provisioning` hangs, pod `ImagePullBackOff` | The node must reach `ghcr.io`. With the `:local` image, run `task image:import` after every rebuild |
| `k8s-desktop` pod `ErrImageNeverPull` or `ImagePullBackOff` | Run `task desktop:image`, it imports `infer-desktop:local` into the cluster |
| `desktop:screenshot` fails with `array index out of bounds` | No session lives on `k8s-desktop` yet. Run `task demo TARGET=k8s-desktop` first |
| `artifacts:list` says the bucket does not exist | No run finished on `k8s-desktop` since `task artifacts:install`. The first run creates it |
| `provisioning` fails with `did not become Ready` | Check `kubectl -n infer-environments describe sandbox` and the pod's logs |
| `session ... is owned by environment` | The session moved. Continue it with `infer daemon run`, or delete its Sandbox with `infer daemon stop` |
| `restoring` fails with `does not apply cleanly` | The project changed after the transfer started. Retry |
| `checkpointing` fails with `not a git checkout with a commit` | A transfer bundles `HEAD`, so the project needs at least one commit |
| The relay drops with `RUN_ERROR` after the laptop slept | The port-forward died. The next `infer daemon run` opens a new one |
| SPIFFE: `fetching an SVID from the SPIRE Workload API` | Keep `task spire:agent` running and start the daemon with `task daemon:start:spiffe` |
| SPIFFE: the pod stays `ContainerCreating` | The CSI driver and SPIRE agent must run on the node: `kubectl get pods -n spire-system` |

Two things to know. The lease a relaying daemon keeps for a remote owner lapses after `lease_ttl` (the
environment's `idle_ttl` plus five minutes by default): a laptop that cannot reach the pod after that may
reclaim the session locally, with the conversation as of the last checkpoint. And a panel request such as
`list_conversations` from another client in the same project dir may be routed to the relay and answer from
the pod's store.
