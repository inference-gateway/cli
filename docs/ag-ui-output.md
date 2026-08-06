# AG-UI Output Format

`infer agent` can emit its stdout stream as [AG-UI protocol](https://github.com/ag-ui-protocol/ag-ui)
events instead of the legacy CLI-specific JSON lines:

```bash
infer agent --output-format ag-ui "fix the failing test"
```

The default (`--output-format json`) is unchanged. With `ag-ui`, stdout carries exclusively
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
| Successful exit | `RUN_FINISHED` with a success outcome; `result` carries the session stats (tokens, cost) |
| Failure or panic | `RUN_ERROR` with the error message and the run id |

Every run is bracketed by `RUN_STARTED` and exactly one terminal `RUN_FINISHED` or `RUN_ERROR`.
The `approval_request` value is the legacy payload (`tool_name`, `tool_args`, `tool_call_id`);
replies are still `approval_response` JSON lines on stdin, exactly as in `json` mode.
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
