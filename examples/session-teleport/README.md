# Session teleport: teleport a daemon session to a Kubernetes workspace

Teleport a running `infer daemon` session from your laptop to a Kubernetes sandbox and pick it up from any
client. The worker process never teleports: the daemon checkpoints the conversation and the project's git
state, a pod running the same `infer` image as a daemon restores them, ownership teleports with a lease, and
every client keeps talking to its own daemon, which relays the thread to the pod. Closing the laptop does
not stop the pod.

The cluster is a local [k3d](https://k3d.io/) cluster on k3s v1.37.1, declared in `Cluster.yaml` and created by
[ctlptl](https://github.com/tilt-dev/ctlptl), running the
[Inference Gateway operator](https://github.com/inference-gateway/operator) on top of the
[agent-sandbox](https://github.com/kubernetes-sigs/agent-sandbox) controller. Every session gets an
`Orchestrator` with `runtime: Sandbox`: a stable pod with two persistent volumes (the workspace and the
conversation store) that the operator suspends when the pod daemon idles out and the next teleport resumes.
Both workspaces authenticate the pod with mTLS from the daemon's own CA: `k8s-dev` runs the plain image,
`k8s-desktop` adds a Linux desktop of its own.

Everything runs against the built-in mock model, so no API key is needed. See [infer daemon](../../docs/daemon.md#workspaces-and-teleports)
for the workspace model and the [Daemon Binding Protocol](../../docs/browser-extension-protocol.md#workspaces)
for the wire frames.

## Prerequisites

- [ctlptl](https://github.com/tilt-dev/ctlptl) and [k3d](https://k3d.io/) v5 (ctlptl drives k3d)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [docker](https://docs.docker.com/get-docker/) (k3d runs on it, `task image:import` builds with it)
- [task](https://taskfile.dev/) and Go 1.26 (the image build)
- `task build` at the repository root: the tasks run that `infer` binary (`INFER=<path>` overrides it)

## Layout

| File | Purpose |
| --- | --- |
| `Cluster.yaml` | The ctlptl cluster: k3d `infer-workspaces` with one agent node, pinned to k3s v1.37.1 |
| `daemon.yaml` | The laptop daemon's config: the binding and the `k8s-dev` and `k8s-desktop` workspaces |
| `desktop/` | The `k8s-desktop` image: Xvfb, Chromium and ffmpeg on the infer image, the browse scenario and the artifact upload hook |
| `k8s/rustfs.yaml` | RustFS, the S3-compatible store the desktop's artifacts are uploaded to, with a 2Gi volume |
| `k8s/namespace.yaml` | The `infer-workspaces` namespace. The pods need no ServiceAccount, the operator parks them |
| `Taskfile.yml` | Every step below |
| `.home/` | Created by the tasks: a throwaway `HOME` with the daemon config and the demo project |

The tasks run the daemon under `.home/` as `HOME`, so your own `~/.infer` stays untouched.

## Quick start: the daemon CA workspace

```bash
# 1. The cluster, agent-sandbox, the Inference Gateway operator and the workspace namespace
task cluster:create

# 2. The infer image built from this checkout, imported into the cluster
task image:import

# 3. In its own shell: the laptop daemon (binding on 127.0.0.1:52790, clear of a daemon of your own on 52789)
task daemon:start

# 4. In another shell: create a conversation, teleport it, continue it on the pod
task demo
```

The workspaces run `ghcr.io/inference-gateway/cli:local`, the image `task image:import` builds from this
checkout, until a release candidate speaks the teleport protocol. Run `task image:import` again after every
change you want on the pod.

`task demo` prints the workspaces first, then the teleport's stages, the Orchestrator and its pod, and the
reply of a turn that ran on the pod:

```text
NAME         KIND        REACHABLE  DETAILS
laptop       local       true       storage=jsonl version=...
k8s-desktop  kubernetes  true       image=infer-desktop:local ...
k8s-dev      kubernetes  true       image=ghcr.io/inference-gateway/cli:local kubernetes=v1.37.1+k3s1 namespace=infer-workspaces orchestrators=0
provisioning
checkpointing
restoring
ready
session now lives on k8s-dev in /workspace/project/<conversation id>
NAME                                                         RUNTIME   READY   AGENTS   MCPS   AGE
orchestrator.core.inference-gateway.com/infer-7eb7f642fa3d   Sandbox   true                    12s
Hello! How can I help?
```

What each step proved:

- `provisioning` checked that the cluster serves Orchestrators, created a Secret with the pod's server
  certificate, its client certificate and a binding token issued by the laptop daemon's CA
  (`.home/.infer/daemon/pki/`), created the `Orchestrator` from the workspace entry, waited for the operator to
  report it `Ready` and opened a port-forward to the pod's binding.
- `checkpointing` stopped the laptop worker, exported the conversation through a worker in the project and
  packed the project's `HEAD` plus its uncommitted and untracked changes.
- `restoring` streamed the checkpoint to the pod daemon over mTLS, which cloned the bundle into
  `/workspace/project/<conversation id>`, imported the conversation through a worker there, claimed the
  lease, and answered `resume_conversation` with a snapshot the laptop checked against the checkpoint.
- `infer daemon run` sent a turn to the laptop daemon, which relayed it to the pod. The reply came from a
  worker running in the pod.

Look around:

```bash
# The conversation and the workspace dir on the pod
POD=$(kubectl -n infer-workspaces get pods -o jsonpath='{.items[0].metadata.name}')
kubectl -n infer-workspaces exec $POD -- sh -c 'ls /workspace/project/*/ && ls /home/infer/.infer/projects/*/conversations'

# A second teleport of the same session is refused: k8s-dev owns it now
HOME=$PWD/.home ../../infer daemon teleport <conversation id> --to k8s-desktop --project-dir $PWD/.home/project

# Stop the laptop daemon (Ctrl+C in its shell), start it again, and the session is still on the pod
task daemon:start
HOME=$PWD/.home ../../infer daemon run <conversation id> "and again" --project-dir $PWD/.home/project
```

After `idle_ttl` (30 minutes in `daemon.yaml`) with no client and no run, the pod daemon exits and the operator
suspends its Orchestrator: `kubectl -n infer-workspaces get orchestrators` shows `READY false`, its `Ready`
condition says `SandboxSuspended`, the pod is gone and the two volumes stay. The next `infer daemon run` or
teleport sets `spec.sandbox.operatingMode` back to `Running`, waits for the new pod and reattaches the volumes.

The pod only idles out when no run is open, so an agent you sent off keeps working with the laptop closed.
The pod daemon leaves whether every agent is done as its termination message, and only then does the
suspension start the workspace's `retention` (24 hours in `daemon.yaml`): the operator then deletes the
Orchestrator with its Sandbox, volumes and Secret, unless a run or a teleport resumed it first. An agent that
waits on an approval or a question, or whose last run failed, is not done, and its Orchestrator stays.
`kubectl -n infer-workspaces get sandbox -o yaml` shows the countdown as `spec.shutdownTime` and the verdict as
the `inference-gateway.com/agents-done` annotation. To delete a session's Orchestrator now, whatever its state:

```bash
HOME=$PWD/.home ../../infer daemon stop <conversation id> --project-dir $PWD/.home/project
```

`stop` releases the lease too, so the session continues from the laptop's copy, as it was before the teleport.

## A computer of its own: the desktop workspace

The `k8s-desktop` workspace gives the session a Linux desktop inside its pod. The `desktop/` image adds Xvfb,
Chromium and ffmpeg to the infer image, and puts an `infer` wrapper first on `PATH` that starts display `:99`
with Chromium on it before the pod daemon, since the operator runs the pod's `infer daemon` by name.
`INFER_COMPUTER_USE_ENABLED` in the workspace's `env` registers the `Computer` tool for the pod's workers, so
after the teleport the agent drives the pod's screen, never your laptop's.

```bash
# 1. Build the desktop image and import it into the cluster
task desktop:image

# 2. With the laptop daemon running: teleport a fresh conversation to the desktop and ask it to browse
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
screenshots, so choose a vision model. The desktop lives as long as the pod: a suspended Orchestrator comes back
with a fresh display and browser, and only the workspace dir and the conversation persist.

## From the chat TUI: infer chat --daemon

`infer chat --daemon` drives the same sessions from the chat TUI. The laptop daemon runs every turn, so the
chat follows the session wherever it teleports and the session keeps running when the TUI closes. The status bar
shows where it runs: `⌂ laptop` here, `⇄ k8s-desktop` on the pod, `⇢ k8s-desktop` while it teleports.

```bash
# 1. With the desktop image imported (task desktop:image): the chat, which starts the laptop daemon when none runs
task chat
```

In the chat:

1. Send `say hello`. The turn runs on a worker of the laptop daemon.
2. Press `↓` to reach the status bar, `→` to the `⌂ laptop` indicator and `enter`, or type `/workspaces`.
   The picker lists every workspace with its reachability and details and marks `laptop` with `● here`.
3. Pick `k8s-desktop` and press `enter`, or type `/teleport k8s-desktop`. The status line counts the four
   stages, `provisioning`, `checkpointing`, `restoring` and `ready`, and the indicator turns into `⇄ k8s-desktop`.
4. Send `browse to example.com`. The turn runs in the pod, and `task desktop:screenshot` in another shell saves
   its screen to `.home/desktop.png`.
5. Quit with `/exit`. The session keeps running on the pod, and the exit line prints its id. Come back to it
   with `task chat -- --session-id <id>`, which runs `infer chat --daemon --session-id <id>` with the demo `HOME`.
6. Bring it home with `/teleport laptop`, or `laptop` in the picker. The pod packs the session, its commits
   fast-forward the checkout in `.home/project`, and its uncommitted and untracked files land there too.

Teleporting back lands only while the checkout is clean or unchanged since the session left, so stash your own
changes first (`git stash -u`, then `git stash pop` afterwards).

## Artifacts on durable storage: RustFS

What the agent produces on its computer outlives the pod. [RustFS](https://rustfs.com/), an open source
S3-compatible object store, runs in the cluster on its own volume, and when the agent is done with a run
everything it placed in `~/.infer/artifacts` or `~/.infer/projects/<project>/artifacts/<session id>` is
uploaded to the `infer-artifacts` bucket under the session id.

```bash
# 1. RustFS in the workspace namespace (S3 API on rustfs:9000, console on rustfs:9001)
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
Orchestrator is suspended or deleted.

The upload is a `post_session` [command hook](../../docs/hooks.md) baked into the desktop image
(`desktop/hooks.yaml`). It runs `desktop/upload-artifacts` after every finished run, which reads the session
id from the hook's stdin and mirrors both artifact directories to `infer-artifacts/<session id>/` with the
MinIO client. The workspace's `env` points recordings at
`~/.infer/artifacts`, allow-lists the hook with `INFER_TOOLS_BASH_ALLOW_APPEND`, and carries the endpoint,
bucket and credentials. A pod without `ARTIFACTS_ENDPOINT` skips the upload. The demo credentials sit in
`daemon.yaml` in plain text. Put real ones under the workspace's `forward_env` instead, so they travel
sealed with the session and never land in the Orchestrator spec.

## Switching the pod to a real model

The image sets `INFER_GATEWAY_URL=http://inference-gateway:8080`, so a pod reaches a gateway Service of
that name in its namespace. Deploy one (the [kubernetes example](../kubernetes/) uses the operator for
this) with your provider keys in a Secret, drop `INFER_GATEWAY_MOCK` and set `INFER_AGENT_MODEL` in the
workspace's `env`. Provider and MCP credentials the laptop's workers use can travel with the session instead:
list them under the workspace's `forward_env`, and a teleport seals them to the pod's certificate for its
workers. They never land in a Kubernetes object.

## Cleanup

```bash
task cleanup
```

## Troubleshooting

| Symptom | Fix |
| --- | --- |
| `the Inference Gateway operator is not installed` | Run `task operator:install`, check `kubectl get crd orchestrators.core.inference-gateway.com` |
| `predates runtime: Sandbox` | The operator is older than v0.28.0. Run `task operator:install` |
| `runtime: Sandbox needs agent-sandbox` | The operator started first. `kubectl -n inference-gateway-system rollout restart deployment` |
| `provisioning` fails with `already exists` | An rc of infer left a bare `Sandbox`. `kubectl -n infer-workspaces delete sandboxes --all` |
| `provisioning` hangs, pod `ImagePullBackOff` | Run `task image:import`, the pod runs the `:local` image built from this checkout |
| `k8s-desktop` pod `ErrImageNeverPull` or `ImagePullBackOff` | Run `task desktop:image`, it imports `infer-desktop:local` into the cluster |
| `desktop:screenshot` fails with `array index out of bounds` | No session lives on `k8s-desktop` yet. Run `task demo TARGET=k8s-desktop` first |
| `artifacts:list` says the bucket does not exist | No run finished on `k8s-desktop` since `task artifacts:install`. The first run creates it |
| `provisioning` fails with `did not become ready` | Check `kubectl -n infer-workspaces describe orchestrator` and the pod's logs |
| `session ... is owned by workspace` | The session teleported. Continue with `infer daemon run`, or delete it remotely with `infer daemon stop` |
| `restoring` fails with `does not apply cleanly` | The project changed after the teleport started. Retry |
| Teleporting back fails with `did not answer the teleport back` | The pod runs an older infer. Run `task image:import`, then `task desktop:image` |
| Teleporting back fails with `changed since the session left it` | `git stash -u`, teleport back, then `git stash pop` |
| `checkpointing` fails with `not a git checkout with a commit` | A teleport bundles `HEAD`, so the project needs at least one commit |
| The relay drops with `RUN_ERROR` after the laptop slept | The port-forward died. The next `infer daemon run` opens a new one |

Two things to know. The lease a relaying daemon keeps for a remote owner lapses after `lease_ttl` (the
workspace's `idle_ttl` plus five minutes by default): a laptop that cannot reach the pod after that may
reclaim the session locally, with the conversation as of the last checkpoint. And a panel request such as
`list_conversations` from another client in the same project dir may be routed to the relay and answer from
the pod's store.
