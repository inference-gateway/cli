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
  (`model`, `mode`, `system_prompt`, `custom_instructions`, `sandbox_directories`, `max_turns`) apply when
  the worker launches.
- **Forwarded frames** - `user_message`, `interrupt`, `user_question_response` and `computer_use_control` go 
  to the client's thread unchanged. A `user_message` relaunches a thread whose worker exited.            
- **Browser commands** - a `browser_command` line on a worker's stdout routes through the `BrowserRelay`      
  (`RouteBrowser`, the binding's extension connection) and the `browser_result` carrying the command's `id` goes             
  back to that worker's stdin, so the worker's Browser tools resolve without binding a port. Commands serialize             
  across threads, because one browser serves them, and the thread's clients never see the frames.        
- **Panel requests** - `list_*`, `select_model`, `set_mode` and `tool_request` run on the client's thread
  when it is in the frame's `project_dir`, else on any live worker there, else on a fresh idle one. The reply
  (`conversations`, `models`, `tool_result`, ...) goes back to the requester only.
- **Approvals** - every CUSTOM `approval_request` reaches the thread's clients. The first `approval_response`
  for a `tool_call_id` goes to the worker, and the other clients get CUSTOM `approval_resolved`.
- **Crashes and idling** - a worker that exits mid-turn ends its open run with a synthesized `RUN_ERROR`. A
  worker nobody follows is stopped after the idle timeout, and daemon shutdown stops every worker.

## How it plugs in

- `domain/` holds the ports: `Threads` (what driving adapters call), `Client` (a connection frames are
  delivered to), `Worker` and `LaunchWorker`.
- The AG-UI binding in `internal/protocols/agui` is a driving adapter: each WebSocket connection is a
  `Client`, and it calls `Handle` per frame and `Detach` on disconnect.
- The Telegram channel becomes a driving adapter too, with one `Client` per chat speaking the same ipc
  frames.
- `infrastructure.LaunchWorker` is the worker adapter. It runs this binary as
  `headless --serve --require-approval` in the project dir, with the thread options as flags and `INFER_`
  env overrides, and it stops the worker with SIGTERM so the worker's gateway, MCP servers and containers
  shut down.
- `cmd/daemon` wires the registry behind the binding when `browser_use` is enabled with the extension
  backend.

## Related

- [agui](../protocols/agui/README.md)
- [Browser Extension Bridge Protocol](../../docs/browser-extension-protocol.md)
- [AG-UI Output Format](../../docs/ag-ui-output.md)
