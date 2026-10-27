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
| AG-UI binding | `daemon.binding.enabled`, else `browser_use.enabled` + `backend: extension` | The clients' WebSocket and the `/artifacts/` route |
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

The binding listens on `ws://127.0.0.1:<port>/ws`. Its switch, port and token live in
`~/.infer/daemon.yaml`, and `browser_use.extension` stays the fallback for the port and the token:

```yaml
# ~/.infer/daemon.yaml
binding:
  enabled: true
  port: 52789
  token: <shared secret>
```

Env overrides follow the usual scheme: `INFER_DAEMON_BINDING_ENABLED`, `INFER_DAEMON_BINDING_PORT`,
`INFER_DAEMON_BINDING_TOKEN`. The wire contract is the
[Daemon Binding Protocol](browser-extension-protocol.md), and the events on it are the
[AG-UI output](ag-ui-output.md). The same listener serves generated images read-only at
`GET /artifacts/<project-slug>/<relative-path>`.

## Session workers

A thread is a project dir plus a conversation id. The daemon runs each thread in its own worker,
`infer headless --serve --require-approval --session-id <id>`, started in the project dir so the
project's `.infer/` config, storage, skills and sandbox apply. Thread options a client sends on
`new_session` or `resume_conversation` (model, mode, system prompt, custom instructions, sandbox
directories, max turns) become the worker's flags and `INFER_` env.

Workers speak the binding's vocabulary on stdio, so the daemon relays frames without translating
them. A client that deletes a conversation sends `stop_thread`, which stops its worker at once. A worker
nobody follows stops 30 seconds after the turn comes back to the user with no run
open, no reply pending and no background job still running. A worker that exits mid-turn ends its run
with `RUN_ERROR` for every client of the thread, and the next `run_agent_input` starts a new one.

A worker's headless subagents are threads too: the worker opens
`subagent-<parent id>-<child id>` over the binding as a client, so the daemon runs the subagent in
a worker of its own and the desktop can follow it live. Without a configured binding the worker
spawns the subagent itself instead.

Scheduled jobs and heartbeat wake-ups do not use workers. Each runs as a one-shot `infer headless`
inside the daemon, one process per run, and no client follows it.

## Starting it

Run it in the foreground from the directory whose config it should load:

```bash
infer daemon
```

With `browser_use.backend: extension`, `infer chat` and a one-shot `infer headless` start the daemon
on demand on their first browser tool call when nothing listens on the binding's port. A daemon
started this way runs in the background with the starting process's config and shares its terminal:
closing the terminal stops it, and the next browser call starts another.

The pid file is `~/.infer/run/daemon.pid`. A second `infer daemon` exits with
`daemon already running (pid N)` while that pid is alive, and a stale file is overwritten. Stop the
daemon by signalling that pid. There is no `stop` or `status` subcommand.

## Logs

Everything lands in `~/.infer/logs/daemon-<date>.log` (`logging.dir` moves the directory). Session
workers and one-shot job runs log JSON to stderr and keep no file of their own: the daemon collects
their stderr into its log, each line tagged with `project_dir`, `conversation_id` and `worker_pid`.
Client connects and disconnects, and the browser extension's attach and detach, are logged there too.
