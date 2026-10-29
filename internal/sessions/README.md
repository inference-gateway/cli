# sessions

**What** - the sessions bounded context: the daemon's thread registry and the supervision of the session
workers that run the threads. A thread is a project dir plus a conversation id, and it runs in one long-lived
`infer headless --serve` worker.
**Why** - `infer daemon` owns every external connection. Clients never spawn `infer headless` themselves,
several clients can follow one thread, and a worker keeps its project's cwd, config and crash isolation.
**How** - `Registry` launches a worker per thread and relays frames without translating them. Client frames
go to the worker's stdin, and worker stdout lines go to the thread's clients, one frame each.

## What the registry decides

- **Following a thread** - `new_session` (a fresh conversation id) and `resume_conversation` (the given `id`)
  make a client follow that thread, and the worker answers with `MESSAGES_SNAPSHOT`. A client follows one
  thread at a time, because bare AG-UI events carry no thread id. The thread options on these frames
  (`model`, `mode`, `system_prompt`, `custom_instructions`, `sandbox_directories`, `max_turns`, `subagent`)
  apply when the worker launches. `subagent` names the parent thread that spawned this one, its depth and its
  tool allowlist, which the worker gets as its subagent env.
- **Forwarded frames** - `run_agent_input`, `interrupt` and `user_question_response` go to the client's
  thread unchanged. A `run_agent_input` relaunches a thread whose worker exited.
- **Resumes** - a run that ends with the `interrupt` outcome leaves its interrupts open on the thread. The
  first `run_agent_input` whose `resume` answers all of them goes to the worker, and the other clients learn
  the decision from the continuation run's `RUN_STARTED`. A resume that misses an open interrupt is refused
  with `RUN_ERROR`, and a later resume for interrupts no longer open is dropped together with any messages it
  carries, so new messages travel in their own `run_agent_input`.
- **Browser commands** - a `browser_command` line on a worker's stdout routes through the `BrowserRelay`
  (`RouteBrowser`, which the daemon wires to the browser context's extension relay) and the `browser_result` carrying the command's `id` goes
  back to that worker's stdin, so the worker's Browser tools resolve without binding a port. Commands serialize
  across threads, because one browser serves them, and the thread's clients never see the frames.
- **Panel requests** - `list_*`, `select_model`, `set_mode` and `tool_request` run on the client's thread
  when it is in the frame's `project_dir`, else on any live worker there, else on a fresh idle one. The reply
  (`conversations`, `models`, `tool_result`, ...) goes back to the requester only.
- **Panel approvals** - a `tool_request`'s CUSTOM `approval_request` reaches its requester. The first
  `approval_response` for a `tool_call_id` goes to the worker, and any other client of the thread that saw it
  gets CUSTOM `approval_resolved`.
- **Stopping a thread** - `stop_thread` names a thread by `project_dir` and `id` and stops its worker at
  once, which is what a client sends when it deletes the conversation. The worker's exit does the usual
  bookkeeping.
- **Agents done** - `AgentsDone` reports whether the last run of every thread the registry ran finished
  without waiting on the user. A pod daemon reads it when it idles out to decide whether its Sandbox's
  retention may start.
- **Crashes and idling** - a worker that exits mid-turn ends its open run with a synthesized `RUN_ERROR`. A
  worker nobody follows is stopped once it sat on the user's turn for the idle timeout with no open run,
  no pending reply and no background job (tracked from its `backgroundTasks` state), and daemon shutdown
  stops every worker.

## How it plugs in

- `domain/` holds the ports: `Client` (a connection frames are delivered to), `Worker`, `LaunchWorker` and
  `BrowserRelay`.
- `Registry.Handle` and `Registry.Detach` are what a driving adapter calls, per frame and on disconnect. This
  context imports no protocol: `cmd/daemon` puts the registry behind the AG-UI binding's handler, and each
  connection is a `Client`.
- The Telegram channel becomes a driving adapter too, with one `Client` per chat speaking the same ipc
  frames.
- A worker's `Agent` tool is a client too: a headless subagent is a thread the parent worker opens over the
  binding as `subagent-<parent id>-<child id>`, runs with `run_agent_input` and leaves when it is done, so the
  daemon owns the subagent's worker and the desktop follows the same thread live. The subagent worker reports
  each finished turn to its parent as a CUSTOM `subagent_turn` event. Without a configured binding the tool
  falls back to spawning `infer headless --keep-alive` itself.
- `Quiesce`, `Deliver`, `Request`, `MovedAway` and `Idle` are the hooks a session transfer uses: end a thread's run and
  stop its worker while keeping its clients, tell a thread's followers something, ask a worker in a project dir one
  panel question, stop a thread whose session left from holding `AgentsDone` back, and learn whether the daemon sits
  idle. The agent context's `transfer` package drives them.
- `infrastructure.LaunchWorker` is the worker adapter. It runs this binary as
  `headless --serve --require-approval` in the project dir, with the thread options as flags and `INFER_`
  env overrides. It stops the worker with an `interrupt` frame and stdin EOF, which works on every platform, so
  the worker's gateway, MCP servers and containers shut down. The worker logs JSON to stderr instead of keeping
  a log file of its own, and the daemon collects its stderr into the daemon log with the thread's
  `project_dir`, `conversation_id` and `worker_pid`.
- `cmd/daemon` wires the registry whenever the binding (`daemon.binding.enabled`, or `browser_use` with the
  extension backend) or channels are enabled.

## Related

- [agui](../protocols/agui/README.md)
- [Daemon Binding Protocol](../../docs/browser-extension-protocol.md)
- [AG-UI Output Format](../../docs/ag-ui-output.md)
