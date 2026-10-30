# agent

**What** - the agent bounded context: what an agent is (its mode, tools, chat events and the ports it calls), the
adapters behind those ports, and the three ways to run one.
**Why** - the loop is the core domain of the CLI, and every other context exists to hand it tools, storage or transport.
**How** - `domain/` is the shared kernel every context may import. `infrastructure/` adapts its ports to the outside
world. `loop/` runs an agent in process, `headless/` runs one as an `infer headless` child, and `transfer/` runs one
on another environment.

## How it plugs in

- `domain/` holds the tool contracts and results, agent mode, chat events and the service ports the loop calls:
  skills, GitHub, command hooks, system reminders, user questions, media and image annotation. The a2a, browser,
  computer and MCP tools implement its tool contracts.
- The capabilities implement the service ports: `skills` the skills service, `github` the issue and setup services,
  `plugins` a command-hook provider.
- `infrastructure/` holds the adapters for the file, frame-source and media ports. The media adapters call the gateway.
- `loop/` is the event-driven state machine that turns a user turn into model calls, tool calls and approvals.
  `agent_event_driven.go` routes events to `states/`, which runs one executor per state, and `agent_state_machine.go`
  defines the allowed transitions. The loop is a conformist to the Inference Gateway SDK: it uses the SDK's message
  and tool-call types directly, so an SDK change reaches the loop without a translation layer.
- `headless/` spawns `infer headless`, streams its stdout and returns the final assistant message. The `Agent` tool,
  the scheduler's cron jobs and heartbeat, and the GitHub setup call it.
- `domain/` also names where an agent runs: the `Environment` port (ensure, reach, probe, suspend, delete) with its three
  implementations in `infrastructure/` (this daemon, a standalone daemon over TLS, a Kubernetes `Sandbox` per session
  under `infrastructure/kubernetes/`), the `Lease` that says which environment owns a session and the `LeaseStore` every
  storage backend implements, and the `Checkpoint` a transfer carries.
- `transfer/` is the daemon's launch seam and its transfer saga. The sessions registry launches every thread through
  `Service.LaunchWorker`, which answers at once with a worker that connects in the background to a process here or to
  the owning environment's daemon, by the lease. `transfer_session` quiesces the thread, packs the checkpoint (conversation
  unless storage is shared, open interrupts, a git bundle plus patch, allowlisted files, credentials sealed with HPKE to
  the target's certificate), streams it to the peer, verifies the restored snapshot and hands the lease over. The
  receiving side restores into `workspace_root`, imports the conversation through a worker there and claims the lease.
  `infrastructure/` also holds the daemon CA, the TLS provider for the `files` and `ca` modes, and the peer
  dialer.
- client-go stays in `infrastructure/kubernetes/`. depguard enforces it.
- The tools context never imports `loop/`. depguard enforces it, because the loop consumes tools.
- `internal/container/container.go` builds the loop and wires every port.

## Related

- [Plan Mode](../../docs/plan-mode.md)
- [Judge Mode](../../docs/judge-mode.md)
- [Subagents](../../docs/subagents.md)
- [infer daemon](../../docs/daemon.md)
- [Daemon Binding Protocol](../../docs/browser-extension-protocol.md)
