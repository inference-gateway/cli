# AG-UI Output Format

`infer headless` can emit its stdout stream as [AG-UI protocol](https://github.com/ag-ui-protocol/ag-ui)
events instead of the legacy CLI-specific JSON lines:

```bash
infer headless --format ag-ui "fix the failing test"
```

The default (`--format json`) is unchanged. With `ag-ui`, stdout carries exclusively
newline-delimited AG-UI events, serialized with the official AG-UI Go SDK
(`github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events`), so any AG-UI client can
decode them with `events.EventFromJSON` without a custom adapter. AG-UI is transport agnostic, so
a subprocess host reading stdout is a fully valid transport.

Every event the CLI writes conforms to the frozen AG-UI 1.0 specification (`spec/1.0/schema.json`
upstream), which the test suite validates against. The wire contract is `protocol_version` 2 on the
daemon binding. CUSTOM events remain the extension point for what the protocol has no event for.
Today that is one: the approval prompt of a panel-initiated `tool_request` (see
[browser-extension-protocol.md](browser-extension-protocol.md)).

## Event mapping

| Agent lifecycle moment | AG-UI event(s) |
| --- | --- |
| Run start | `RUN_STARTED` with `threadId` = session id, `runId` = fresh per-run id, then `STATE_SNAPSHOT` seeding every state key |
| Resume via `--session-id` | `MESSAGES_SNAPSHOT` of the restored history, right after the snapshot |
| Local A2A agent starting | `ACTIVITY_SNAPSHOT` `agent_status` keyed `agent:<name>`, content `name`, `state`, `message`, `done`/`total`. See [Activity](#activity) |
| User / assistant message | `TEXT_MESSAGE_START` / `TEXT_MESSAGE_CONTENT` / `TEXT_MESSAGE_END`, role on START |
| Reasoning | `REASONING_MESSAGE_START` / `_CONTENT` / `_END`, role `reasoning`, before the same message's text |
| Assistant tool call | `TOOL_CALL_START` / `TOOL_CALL_ARGS` / `TOOL_CALL_END` (parented to the assistant message when it has text) |
| Tool result | `TOOL_CALL_RESULT` whose `content` is the raw JSON execution result (no `"Result of tool call:"` prefix) |
| LLM step completes | `STATE_DELTA` patching `usage` |
| Todo-list change | `STATE_DELTA` patching `todos` |
| Background job submitted or finished | `STATE_DELTA` patching `backgroundTasks` (`running`, `jobs`) |
| Background job finished | The landed note as a text message with its conversation role, first line `[<Kind> Completed\|Failed: <label>]` |
| Screen recording starts or ends | `STATE_DELTA` patching `screenRecording` with `active`, and the frame while it runs |
| Computer tool pointer or keyboard action | `ACTIVITY_SNAPSHOT` `computer_use` keyed `computer_use:<toolCallId>`, the action and its screen coordinates. See [Activity](#activity) |
| LLM judge verdict (`auto-with-judge`) | `ACTIVITY_SNAPSHOT` `judge_verdict` keyed `judge:<tool>:<turn>`. See [Activity](#activity) |
| Approval request (`--require-approval`) | `RUN_FINISHED` with outcome `interrupt`, the interrupt's `reason` `tool_call` and its `toolCallId`. See [Interrupts](#interrupts) |
| AskUserQuestion form | `RUN_FINISHED` with outcome `interrupt`, the interrupt's `reason` `input_required` and a `responseSchema` |
| Successful exit | `RUN_FINISHED` with outcome `success`, `usage` and `result` |
| Stop (`interrupt` frame) | `RUN_FINISHED` with outcome `cancelled` and `usage` |
| Failure or panic | `RUN_ERROR` with the error message and the `usage` accrued so far |

Every run is bracketed by `RUN_STARTED` and exactly one terminal `RUN_FINISHED` or `RUN_ERROR`, and
no event is written outside a run. A failure before any run starts (gateway down, unknown model)
is one bare `RUN_ERROR`.

## Activity

`ACTIVITY_SNAPSHOT` carries progress a client keeps as its own entry, replaced on every snapshot
with the same `messageId`:

| `activityType` | `messageId` | `content` |
| --- | --- | --- |
| `agent_status` | `agent:<name>` | `name`, `state`, `message`, pull progress `done`/`total` |
| `judge_verdict` | `judge:<tool>:<turn>` | `tool`, `model`, `decision`, `reason`, `turn` |
| `computer_use` | `computer_use:<toolCallId>` | `toolCallId`, `action`, pointer `x`/`y` in screen coordinates, `screenWidth`, `screenHeight` |

A computer-use action is written just before the Computer tool performs a
pointer (`move`, `click`, `double_click`, `triple_click`) or keyboard (`type`,
`key`) action. Keyboard actions carry no `x` and `y`; the pointer coordinates
are already scaled to the screen, and `screenWidth`/`screenHeight` give the
space they are relative to. The entry is replaced on every snapshot, so a
client keeps one action entry per tool call.

Agent boot progress observed before the first run opens is held and written right after that run's
`RUN_STARTED`, so no event precedes a run.

`backgroundTasks.jobs` entries carry `id`, `kind`, `label`, `description`, `detail`, `status` and
`started_at`.

## State

The run's state is one object. The `STATE_SNAPSHOT` after `RUN_STARTED` carries every key, and each
change is a `STATE_DELTA` with one JSON Patch `add` on that key, so a change to one key never
replaces the others:

| Key | Value |
| --- | --- |
| `todos` | The todo list, `[]` until the agent writes one |
| `usage` | The cumulative token usage, the same list the terminal event carries, `[]` until the first model request |
| `backgroundTasks` | `{"running": <count>, "jobs": [...]}` |
| `screenRecording` | `active` (bool); while a recording runs also `path`, `region` `{x, y, width, height}`, `frameWidth`, `frameHeight` |

While a recording runs, its `region` and `frameWidth`/`frameHeight` are in the
frame space the recorder captures - the same space screenshots and their
annotations use - so a client can overlay it without rescaling. When it is off,
only `active` remains.

A continuation run (see below) opens with a `STATE_SNAPSHOT` seeded from the state the interrupted
run left, so a client keeps one state object across the pair.

## Usage and result

After a run that made at least one model request, the terminal event carries `usage`: one
`TokenUsage` entry with `model`, `inputTokens`, `outputTokens` and `cachedInputTokens`, cumulative
across the session (not just this run). `RUN_FINISHED.result` keeps what `usage` has no field for.
When no model request was made the event has neither; `contextWindow` is omitted when the model's
context window is unknown:

| Key | Meaning |
| --- | --- |
| `totalToolCalls` | Tool calls issued across the session |
| `cost` | Total session cost, in the configured currency |
| `contextWindow` | Model context window in tokens (omitted when unknown) |

## Interrupts

A tool call that needs approval and an `AskUserQuestion` form both suspend the run the AG-UI way: the
run ends with `RUN_FINISHED` whose `outcome` is `interrupt` and lists what the run waits on. The
answer is a resume entry on the next run, sent as a `run_agent_input` frame on stdin, and the
continuation run writes the `TOOL_CALL_RESULT` for the same `toolCallId`:

```json
{"type":"RUN_FINISHED","threadId":"s1","runId":"r1","outcome":{"type":"interrupt","interrupts":[{"id":"call-1","reason":"tool_call","toolCallId":"call-1"}]}}
```

```json
{"type":"run_agent_input","input":{"resume":[{"interruptId":"call-1","status":"resolved"}]}}
```

- `status` `resolved` approves the tool call, `cancelled` declines it.
- A question's interrupt has `reason` `input_required` and a `responseSchema` describing the
  `payload` the answer carries: `{"answers": [{header, question, selectedLabels, otherText}]}`,
  the AskUserQuestion tool's own shape. `cancelled` dismisses the form and the tool takes its
  dismissed path.
- A resume must answer every open interrupt of the run. The daemon refuses one that misses an
  interrupt with `RUN_ERROR`, keeps the first resume for a thread and drops the later ones.
- `RecordStart` always requires approval outside auto-accept mode, so run with
  `--require-approval` to receive it as an interrupt; without an approver it is blocked.

The legacy `approval_response` and `user_question_response` stdin lines of `json` mode are still
accepted by the worker, but the resume entry is the contract.

A running recording is a `backgroundTasks` job with kind `recording`, and the run waits for it
(up to `a2a.task.agent_mode_max_wait_seconds`) instead of finishing, so send the stop request as a
`run_agent_input` on stdin. A recording still running when the run ends anyway is finalized as
the process exits, after the terminal event, so no `screenRecording` patch with `active: false`
follows it: clear the indicator on `RUN_FINISHED` or `RUN_ERROR`.

Whole messages are emitted as single-delta triads (the synchronous headless loop produces complete
messages); live token streaming is a planned follow-up.

## Desktop sidecar wiring

A Tauri (or any subprocess-hosting) app consumes the stream by spawning the CLI as a sidecar and
feeding each stdout line to its AG-UI client:

```ts
import { Command } from "@tauri-apps/plugin-shell";

const cmd = Command.sidecar("binaries/infer", [
  "headless", "--format", "ag-ui", task,
]);
cmd.stdout.on("data", (line) => {
  const event = JSON.parse(line); // an AG-UI BaseEvent, e.g. { type: "RUN_STARTED", ... }
  dispatchAgUiEvent(event);      // feed your AG-UI client / transformer
});
await cmd.spawn();
```

To resume a thread, pass the `threadId` from `RUN_STARTED` back as `--session-id` on the next
invocation; the new run starts with a `MESSAGES_SNAPSHOT` so the client can render prior history.

## Serve worker

`infer headless --serve --session-id <id>` keeps one long-lived worker per thread instead of one process per
prompt. It takes no task and implies `--format ag-ui`. Stdin carries app frames (lowercase `type`) and stdout
carries AG-UI events (uppercase `type`), the vocabulary the daemon's WebSocket binding speaks, so a host relays
lines without translating them.

| Stdin frame | Effect |
| --- | --- |
| `{"type":"run_agent_input","input":{...}}` | Starts a run with the new messages, queued mid-run. `resume` answers interrupts. Empty input resumes |
| `{"type":"interrupt"}` | Cancels the running turn, which ends with `RUN_FINISHED` outcome `cancelled` |
| `{"type":"browser_result","id":"...",...}` | Answers the `browser_command` carrying the same `id` |
| `new_session`, `resume_conversation` | Answered with a `MESSAGES_SNAPSHOT` of the worker's conversation |
| `list_*`, `select_model`, `set_mode`, `tool_request` | Answered with the [panel frames](browser-extension-protocol.md) for the worker's dir |
| `approval_response` | Answers a pending `tool_request`'s CUSTOM `approval_request` |

`input` is a `RunAgentInput`. `messages` holds the new messages only, since the worker owns the
history. A user message's `content` is a string or content parts: `{"type":"text","text":...}` and
`{"type":"image","mimeType":...,"data":"<base64>","filename":...}`. Each file is saved to the project's
tmp dir. PNG, JPEG, GIF and WebP images reach the model as image parts, and other files as a note naming
the saved path. Files over 10 MiB are skipped.

Each turn is one AG-UI run: `RUN_STARTED` with `threadId` set to the conversation id, then exactly one
`RUN_FINISHED` or `RUN_ERROR`. Only the first run of a resumed session opens with `MESSAGES_SNAPSHOT`. `select_model`
switches the model the next turns use. Without `--model` and `agent.model`, the worker falls back to the gateway's
first model, so it can answer panel frames in an unconfigured project.

Pausing computer use is stopping the run with `interrupt` and continuing it with an empty
`run_agent_input`, which starts a new run on the thread.

With `browser_use.backend: extension` the worker binds no port. A browser tool writes a
[`browser_command`](browser-extension-protocol.md) line on stdout and waits for the `browser_result` line with
the same `id` on stdin, so the host relays both to the extension.

Stdin EOF lets the running turn and any queued messages finish, then shuts the worker down together with the
gateway, MCP servers and containers it started. Send `interrupt` first for a faster stop.
