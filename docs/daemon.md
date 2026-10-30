# infer daemon

`infer daemon` is the long-lived hub every external system reaches the agent through. The desktop
app and the opentask extension connect to its AG-UI WebSocket binding, Telegram drives it in
process, `infer chat` and a standalone `infer headless` reach the user's browser through it,
scheduled jobs and heartbeat wake-ups run inside it, and a peer agent can task it through its
A2A server. One daemon serves every project on the machine.

```text
desktop app ───────────┐
opentask extension ────┼── AG-UI WebSocket binding ──┐
infer chat / headless ─┘   (browser clients)         ├── infer daemon ── session workers
Telegram ──── in-process client of the registry ─────┘        │          (one per thread, AG-UI over stdio)
                                                              ├── scheduler and heartbeat
                                                              │   (one infer headless per job)
                                                              └── one log
```

## Subsystems

The daemon hosts whichever of these the config enables, and refuses to start when none is:

| Subsystem | Switch | Notes |
| --- | --- | --- |
| Channels | `channels.enabled` | Telegram today. See [Channels](channels.md) |
| Scheduler | `tools.schedule.enabled` | Skipped with `scheduler.backend: github`. See [Scheduling](scheduling.md) |
| Heartbeat | `heartbeat.enabled` | See [Heartbeat](heartbeat.md) |
| GitHub artifact poller | `scheduler.backend: github` and `artifacts.enabled` | Pulls finished workflow runs back into conversations |
| AG-UI binding | `daemon.binding.enabled`, else `browser_use.enabled` + `backend: extension` | Clients, peer daemons and `/artifacts/` |
| Environments | `daemon.environments` | Where sessions can be teleported to, see [Environments and teleports](#environments-and-teleports) |
| A2A server | `a2a.server.enabled` | One `infer headless` per task, bearer token required. See [The A2A server](#the-a2a-server) |

Shutdown is SIGINT or SIGTERM. The daemon closes the binding, stops every session worker, then
the poller, the A2A server, heartbeat, scheduler and channels, each with a 30 second budget.

## The A2A server

With `a2a.server.enabled: true` the daemon serves this agent to peers over A2A. Every accepted task
runs as one `infer headless --prompt-only` subprocess in the daemon's working directory, in
`a2a.server.mode` (`standard` by default), and is bounded by `a2a.task.agent_mode_max_wait_seconds`
so a hung run cannot hold the one-at-a-time queue. The bearer token is required, and it is a
credential: set it through `INFER_A2A_SERVER_TOKEN` (for example from `.env`) rather than writing it
into `config.yaml`. Treat `mode: auto` as handing unrestricted bash to every holder of that token.

## The binding

The binding listens on `ws://127.0.0.1:<port>/ws`. Its switch, host, port, token and TLS live in
`~/.infer/daemon.yaml`, and `browser_use.extension` stays the fallback for the port and the token:

```yaml
# ~/.infer/daemon.yaml
binding:
  enabled: true
  host: 127.0.0.1
  port: 52789
  token: <shared secret>
  tls:
    mode: off
```

Env overrides follow the usual scheme: `INFER_DAEMON_BINDING_ENABLED`, `INFER_DAEMON_BINDING_HOST`,
`INFER_DAEMON_BINDING_PORT`, `INFER_DAEMON_BINDING_TOKEN`, `INFER_DAEMON_BINDING_TLS_MODE`. The wire
contract is the [Daemon Binding Protocol](browser-extension-protocol.md), and the events on it are the
[AG-UI output](ag-ui-output.md). The same listener serves generated images read-only at
`GET /artifacts/<project-slug>/<relative-path>`.

A binding on any host but loopback must serve TLS. `tls.mode` picks how:

| Mode | Server side | Who it trusts |
| --- | --- | --- |
| `off` | plain `ws://`, loopback only | the token |
| `files` | `cert_file` and `key_file` | the token, or client certificates signed by `ca_file` when `client_auth: true` |
| `ca` | a certificate the daemon's own CA issues (`~/.infer/daemon/pki/`) | client certificates from that CA, the token stays optional |

With `ca` the token may be empty, since every connection carries a verified certificate.
The `infer daemon environments`, `teleport`, `run` and `stop` commands dial the local binding over whichever
mode it serves, with the token or, when it requires them, a client certificate.

An enabled binding with neither a token nor a client-certificate mode uses this machine's local token: the
daemon and its local clients share one generated into `~/.infer/daemon/binding.token`, readable only by its
owner, on first use. `infer chat --daemon` turns the binding on whatever `daemon.yaml` says, and a daemon a
client starts on demand serves it too, so daemon mode needs no `daemon.yaml` at all.

## Environments and teleports

An environment is where a session's agent runs. This daemon is one, named by `environment` (default the
hostname), and `daemon.yaml` registers the others:

```yaml
# ~/.infer/daemon.yaml
environment: laptop
workspace_root: ~/.infer/workspaces
idle_exit: 0s
environments:
  - name: laptop
    kind: local
  - name: k8s-dev
    kind: kubernetes
    context: k3d-infer-environments
    namespace: infer-environments
    image: ghcr.io/inference-gateway/cli:latest
    storage: 5Gi
    idle_ttl: 2h
    retention: 168h
    env:
      INFER_GATEWAY_URL: http://inference-gateway:8080
    forward_env:
      - GITHUB_TOKEN
  - name: workstation
    kind: daemon
    address: workstation.lan:52789
    token: ${WORKSTATION_TOKEN}
    tls:
      ca_file: ~/.infer/daemon/workstation-ca.crt
```

- `kind: daemon` is a standalone `infer daemon` reached over TLS. `tls.ca_file` pins its certificate,
  without one the system roots verify it, and `tls.cert_file` with `tls.key_file` present a client
  certificate when it requires one. Its `environment` must match the entry's `name`.
- `kind: kubernetes` runs one agent-sandbox `Sandbox` per session in `namespace`, from `image`, as an
  `infer daemon` whose binding listens on the pod network. The Sandbox keeps a `workspace` and a `state`
  volume of `storage` each, so a session survives its pod. This daemon's CA issues the pod's server
  certificate and a client certificate into a Secret, and the pod daemon's binding requires them. The pod
  daemon idles out after `idle_ttl`, suspends its own Sandbox first, and the next teleport resumes it. `env`
  is the pod daemon's non-secret environment, such as the gateway URL. `address` replaces the
  port-forward when an Ingress or Gateway exposes the pods.
- `retention` deletes a suspended Sandbox, its volumes and its Secret once it sat suspended that long,
  through agent-sandbox's `shutdownTime`. It only starts when every agent on the pod is done: its last
  run finished without waiting on an approval or a question. A run still going keeps the pod up, and a
  run that waits on the user or ended in an error keeps the Sandbox until `infer daemon stop` deletes
  it. Resuming the Sandbox clears the countdown. The pod records its verdict in the
  `inference-gateway.com/agents-done` annotation, so a later pod that ran nothing keeps it. Without a
  `retention` a suspended Sandbox stays until the session is stopped. A relay never recreates a Sandbox
  its retention deleted: the session falls back to this machine's copy once its lease lapsed.
- `forward_env` names variables of this daemon's environment that a teleport seals to the target's
  certificate for its workers. They never land in a Kubernetes object.
- `workspace_root` is where a daemon restores teleported projects: `<root>/<project slug>/<conversation id>`.
- `idle_exit` stops a daemon that had no thread and no client for that long. Zero keeps it running,
  except for a daemon a client starts on demand, which stops after 30 minutes. Pod daemons get `idle_ttl`.

A teleport sends a session to another environment between turns: the source daemon interrupts an open run, stops the worker so
storage is flushed, exports the conversation unless both daemons share a storage backend, bundles the
project's `HEAD` with a patch of the uncommitted and untracked changes (minus what `sandbox.yaml`
denies) and the files the request lists, and streams that checkpoint to the target. The target restores
it, imports the conversation through a worker in the new workspace, claims the session's lease and
answers `resume_conversation` with a snapshot the source checks against the checkpoint before it
reports `ready`. From then on the session lives on the target: clients that resume through the source
are relayed there, a client that reaches a daemon-kind environment resumes there directly, and killing the
source does not stop the target.

The restored clone keeps the source's `origin`, minus any credentials an http(s) URL embeds, so the agent
on the target can push to the same remote. Credentials never travel in that URL, so forward a token
through `forward_env` instead.

Ownership is a lease stored next to the conversations: `environment`, `workspace`, `teleport_id` and when
it lapses. A worker starts only on a session this daemon owns or nobody does, relays to the owner
otherwise, and reclaims a lapsed lease whose owner cannot be reached. The owner renews its lease every
30 seconds while a worker runs, a relaying daemon refreshes its own row for the owner for the
environment's `lease_ttl`, and a teleport is a compare-and-swap keyed by `teleport_id`, so a retry is a
no-op once it succeeded. With `storage.type` jsonl the leases are machine-local
(`~/.infer/leases.json`), with a shared Postgres or Redis every daemon sees them. The wire frames are in
the [Daemon Binding Protocol](browser-extension-protocol.md#environments).

Two daemons that teleport sessions between them should point `storage.type` at a backend they share -
Postgres, Redis or D1. On jsonl or memory each machine keeps its own lease rows, so the one-owner
guarantee holds only through the source daemon's final claim and a crash mid-teleport can leave both
daemons believing they own the session. A teleport on a per-machine backend still runs, with a warning
in the daemon's log.

The daemon's subcommands drive this from a shell:

```bash
infer daemon environments                                 # who is reachable
infer daemon teleport <conversation-id> --to k8s-dev      # teleport a session, follow the stages
infer daemon run <conversation-id> "continue please"      # one turn through the daemon, wherever it runs
infer daemon stop <conversation-id>                       # stop the agent and delete its remote runtime
```

A teleport starts on the daemon that owns the session. A daemon that only relays a session refuses to
teleport it with `session <id> is owned by environment <name>`, with one exception: it brings back a session
that left it. `infer daemon teleport <conversation-id> --to <this daemon>` asks the owner to quiesce the
worker and pack the conversation and the workspace, and streams that checkpoint back over the connection
the daemon opened. The workspace lands on the checkout the session left: the pod's commits fast-forward its
branch, then the pod's uncommitted patch and untracked files apply.

A teleport back lands only while the checkout is clean or unchanged since the session left, which the daemon
fingerprints into `~/.infer/daemon/origins/` when a session teleports away. A changed checkout is refused with
`changed since the session left it`, so stash those changes (`git stash -u`), teleport back, then
`git stash pop`. The checkout's own changes wait in a stash until the landing, the lease and the
conversation import all succeed, and any failure undoes all three, so the session stays with its owner.
Untracked files the sandbox policy denies are never touched. The remote runtime stays behind and suspends
after its `idle_ttl`. A pod that runs an infer too old to send sessions back fails the teleport within 15
seconds.

### Daemons on other machines

A VPN such as [Tailscale](https://tailscale.com/) gives two machines network identity and encryption, so
the daemons need no identity system of their own. On the remote machine, keep the binding on loopback
with a token and let [Tailscale Serve](https://tailscale.com/kb/1242/tailscale-serve) publish it under
the machine's `*.ts.net` name with a certificate the system roots trust:

```bash
tailscale serve --bg --https=443 http://127.0.0.1:52789
```

Register it with that name and no `tls`:

```yaml
  - name: workstation
    kind: daemon
    address: workstation.example.ts.net:443
    token: ${WORKSTATION_TOKEN}
```

A grant in the tailnet policy decides which machines reach it. `forward_env` needs the remote daemon to
serve TLS itself (`files` or `ca`): a teleport seals the variables to the certificate the peer presents,
and behind Serve the key of that certificate belongs to Tailscale, not to the daemon.

## Session workers

A thread is a project dir plus a conversation id. The daemon runs each thread in its own worker,
`infer headless --serve --require-approval --session-id <id>`, started in the project dir so the
project's `.infer/` config, storage, skills and sandbox apply. Thread options a client sends on
`new_session` or `resume_conversation` (model, mode, system prompt, custom instructions, sandbox
directories, max turns) become the worker's flags and `INFER_` env.

Workers speak the binding's vocabulary on stdio, so the daemon relays frames without translating
them. A client that deletes a conversation sends `stop_thread`, which stops its worker at once and
deletes the session's runtime on the remote environment that owns it. A worker
nobody follows stops 30 seconds after the turn comes back to the user with no run
open, no reply pending and no background job still running. A worker that exits mid-turn ends its run
with `RUN_ERROR` for every client of the thread, and the next `run_agent_input` starts a new one.

A worker's headless subagents are threads too: the worker opens
`subagent-<parent id>-<child id>` over the binding as a client, so the daemon runs the subagent in
a worker of its own and the desktop can follow it live. Without a configured binding the worker
spawns the subagent itself instead.

A thread whose session another environment owns gets no local worker: the daemon relays its frames to the
owning environment's daemon instead, see [Environments and teleports](#environments-and-teleports).

Scheduled jobs and heartbeat wake-ups do not use workers. Each runs as a one-shot `infer headless`
inside the daemon, one process per run, and no client follows it.

## The chat TUI: infer chat --daemon

`infer chat --daemon`, or `chat.daemon: true` (`INFER_CHAT_DAEMON`), runs the chat TUI as a client of the
local daemon instead of its own agent loop. Every turn runs on the daemon's thread for the project, in a
worker on this machine or relayed to the environment that owns the session, so the TUI follows a session
wherever it lives and the session keeps running when the TUI exits. The exit line prints how to come back:
`infer chat --daemon --session-id <id>` reattaches from any machine whose daemon reaches the owner, and an
approval or a question left open is asked again on reattach. The in-process chat stays the default.

A fresh session opens the model picker unless `agent.model` is set, and a reattached one keeps its model.
The status bar shows where the session runs: `⌂ laptop` on this machine's daemon, `⇄ k8s-dev` on another
environment, `⇢ k8s-dev` while it teleports and `· offline` while the daemon is unreachable, which the TUI
reconnects to with backoff. Selecting that indicator, `/environments`, or `/teleport` without a target opens
a picker of every environment with its reachability and details, and `enter` teleports the session there.
`/teleport <environment> [file...]` teleports it directly, carrying the files git does not track. A teleport is
refused while a turn runs, `esc` cancels a running one, and from a remote environment the session teleports
only back to this machine.

| Feature | In a daemon chat |
| --- | --- |
| Turns, streaming, reasoning and tool cards | Run on the worker |
| Approvals, questions, plan approval, `esc`, the queue, model switch, agent mode, pasted images, skills | Run on the worker |
| `!cmd` and `!!Tool(...)` | Run on the worker as a turn of their own |
| `/new`, `/clear`, `ctrl+n` | Start a new thread on this machine's daemon |
| `/conversations` | Lists the sessions where the current one runs, with the environment each one runs on now |
| `@file` | Inlines this machine's file on a local session. On a remote one `@` lists no files and the reference goes to the agent's workspace |
| `/diff`, `/explorer`, the git branch label | Read this machine's checkout, so they are off while the session runs on another environment |
| `/compact` | Off, the worker compacts the session itself at `compact.auto_at` |
| `/effort` | Off, the worker's config sets the reasoning effort |
| `/tasks`, `ctrl+b` and rewinding or editing the history | Off, the conversation and its commands live on the worker |
| Model-backed custom shortcuts and next-step suggestions | Off, the TUI runs no model of its own |
| `/copy`, `/context`, `/cost`, `/help`, `/theme`, `/stats`, `/traces`, `/tools`, `/agents`, `#issue` | Work in the TUI |

An unavailable feature says why in the status line. The TUI's `INFER_` overrides reach the worker only as its
model and agent mode, and notes only the TUI shows, such as `/cost` output, are gone after the next turn,
when the TUI reloads the conversation the worker stored. A pod image older than the daemon may lack the
richer interrupts, tool progress and plan approval, and cannot send a session back.

## Starting it

Run it in the foreground from the directory whose config it should load:

```bash
infer daemon
```

With `browser_use.backend: extension`, `infer chat` and a one-shot `infer headless` start the daemon
on demand on their first browser tool call when nothing listens on the binding's port, and the Agent
tool and `infer chat --daemon` do the same for their threads. A daemon started this way runs in the
background with the starting process's config, in a session of its own, so it and the threads it runs
survive closing the terminal. It stops after 30 minutes with no thread and no client, unless
`idle_exit` sets its own idle time. `infer daemon --idle-exit <duration>` gives a daemon you start by
hand the same fallback.

A daemon starts its session workers from its own binary, so it stops once that binary is gone, as a
`go run` build is when its run ends. The next client then starts one from its own binary, and the
sessions resume where they left off.

The pid file is `~/.infer/run/daemon.pid`, and the daemon holds an OS lock on it while it runs. A
second `infer daemon` exits with `daemon already running (pid N)` while that lock is held, also when
two start at once, and a file a crashed daemon left behind is taken over. Stop the daemon by
signalling that pid. There is no `stop` or `status` subcommand.

## Logs

Everything lands in `~/.infer/logs/daemon-<date>.log` (`logging.dir` moves the directory). Session
workers and one-shot job runs log JSON to stderr and keep no file of their own: the daemon collects
their stderr into its log, each line tagged with `project_dir`, `conversation_id` and `worker_pid`.
Client connects and disconnects, and the browser extension's attach and detach, are logged there too.
