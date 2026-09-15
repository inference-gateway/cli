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

## Event mapping

| Agent lifecycle moment | AG-UI event(s) |
| --- | --- |
| Session start | `RUN_STARTED` with `threadId` = session id, `runId` = fresh per-invocation id |
| Resume via `--session-id` | `MESSAGES_SNAPSHOT` of the restored history, right after `RUN_STARTED` |
| User / assistant message | `TEXT_MESSAGE_START` / `TEXT_MESSAGE_CONTENT` / `TEXT_MESSAGE_END`, role on START |
| Assistant tool call | `TOOL_CALL_START` / `TOOL_CALL_ARGS` / `TOOL_CALL_END` (parented to the assistant message when it has text) |
| Tool result | `TOOL_CALL_RESULT` whose `content` is the raw JSON execution result (no `"Result of tool call:"` prefix) |
| Todo-list change | `STATE_SNAPSHOT` with `{"todos": [...]}` |
| Approval request (`--require-approval`) | `CUSTOM` event named `approval_request` carrying the legacy payload |
| AskUserQuestion form | `CUSTOM` event named `user_question_request` with `tool_call_id` and `questions` |
| Local A2A agent starting (before `RUN_STARTED`) | `CUSTOM` event `agent_status` with `name`, `state`, `message`, and pull progress `done`/`total` |
| Background job finished | `CUSTOM` event `queued_message`; `content` is the landed note, first line `[<Kind> Completed\|Failed: <label>]` |
| Background job submitted or finished | `CUSTOM` event `background_tasks` with `running` and `jobs` (id, kind, label, description, detail, status) |
| Successful exit | `RUN_FINISHED` with a success outcome; `result` carries the session stats (keys below) |
| Failure or panic | `RUN_ERROR` with the error message and the run id |

Every run is bracketed by `RUN_STARTED` and exactly one terminal `RUN_FINISHED` or `RUN_ERROR`.

## `RUN_FINISHED` result

After a run that made at least one model request, `RUN_FINISHED.result` carries the per-session totals,
cumulative across the session (not just this run). When no model request was made the event has no
`result`; `contextWindow` is omitted when the model's context window is unknown:

| Key | Meaning |
| --- | --- |
| `inputTokens` | Total input tokens across the session |
| `outputTokens` | Total output tokens across the session |
| `cacheReadTokens` | Tokens served from the prompt cache |
| `totalToolCalls` | Tool calls issued across the session |
| `cost` | Total session cost, in the configured currency |
| `lastInputTokens` | Input tokens of the most recent request |
| `contextWindow` | Model context window in tokens (omitted when unknown) |

The `approval_request` value is the legacy payload (`tool_name`, `tool_args`, `tool_call_id`);
replies are still `approval_response` JSON lines on stdin, exactly as in `json` mode.

When the agent calls `AskUserQuestion`, the `user_question_request` value carries `tool_call_id` and
`questions` - the tool's own array of `{header, question, options: [{label, description}], multiSelect}`.
The host renders the form and answers with one JSON line on stdin, either the collected answers or a
dismissal; the run then continues with the answers in the tool result. In `text` format no form is
available and the tool returns its degraded result instead:

```json
{"type":"user_question_response","tool_call_id":"call-1","answers":[{"header":"Scope","question":"Which?","selectedLabels":["Desktop"],"otherText":""}]}
{"type":"user_question_response","tool_call_id":"call-1","cancelled":true}
```

Whole messages are emitted as single-delta triads (the synchronous headless loop produces complete
messages); live token streaming is a planned follow-up.

## Desktop sidecar wiring

A Tauri (or any subprocess-hosting) app consumes the stream by spawning the CLI as a sidecar and
feeding each stdout line to its AG-UI client:

```ts
import { Command } from "@tauri-apps/plugin-shell";

const cmd = Command.sidecar("binaries/infer", [
  "agent", "--output-format", "ag-ui", task,
]);
cmd.stdout.on("data", (line) => {
  const event = JSON.parse(line); // an AG-UI BaseEvent, e.g. { type: "RUN_STARTED", ... }
  dispatchAgUiEvent(event);      // feed your AG-UI client / transformer
});
await cmd.spawn();
```

To resume a thread, pass the `threadId` from `RUN_STARTED` back as `--session-id` on the next
invocation; the new run starts with a `MESSAGES_SNAPSHOT` so the client can render prior history.
