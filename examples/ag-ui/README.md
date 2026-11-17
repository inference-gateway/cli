# AG-UI

`infer headless --format ag-ui` prints a run as [AG-UI](https://github.com/ag-ui-protocol/ag-ui) protocol events - the
format an embedding host reads. This example consumes it with one small Go program, [`main.go`](main.go), against the
[tokenless](https://github.com/inference-gateway/tokenless) mock scripted in [`scenarios.yaml`](scenarios.yaml), so it
runs offline with no API key. You need [Task](https://taskfile.dev) and a Go toolchain.

See [AG-UI Output Format](../../docs/ag-ui-output.md) for the full event reference this README maps into.

## Run it

`task` lists the tasks. The mock model is prompted to make one tool call (a `Grep` over this README) so the stream shows
the complete shape of a run.

| Task | What happens |
| --- | --- |
| `task stream` | Runs `infer headless --format ag-ui --no-save` and prints the run's events by type |
| `task serve` | Starts `infer headless --serve` on a fixed session id. Two `run_agent_input` frames in on stdin, one run each, then stdin closes |
| `task serve:approval` | Same worker with `--require-approval`: the `Write` call suspends the run as an `interrupt`, answered over stdin |
| `task clean` | Removes the marker file the approval variant wrote |

The tasks use the `infer` on your `PATH`. To use a local build instead, run `task build` in the repository root and pass
it in, for example `task stream INFER=../../infer`.

### What to expect

`task stream` (the program filters the events it does not name. Text arrives as one `TEXT_MESSAGE_CONTENT` per chunk):

```text
RUN_STARTED threadId=<session id> runId=<per-run id>
TOOL_CALL_START <id> Grep
TOOL_CALL_RESULT <id> {"tool_name":"Grep" ... "arguments":{"head_limit":3,"output_mode":"content",
 "path":"README.md","pattern":"AG-UI"} ...}
TEXT_MESSAGE_CONTENT The matches above
TEXT_MESSAGE_CONTENT  are the AG-UI references ...
RUN_FINISHED success
```

`task serve`:

```text
RUN_STARTED threadId=ag-ui-serve-demo runId=<first run id>
TEXT_MESSAGE_CONTENT Hello from run one.
RUN_FINISHED success
-- next frame --
RUN_STARTED threadId=ag-ui-serve-demo runId=<second run id>
TEXT_MESSAGE_CONTENT Hello from run two.
RUN_FINISHED success
```

`task serve:approval`:

```text
RUN_STARTED threadId=ag-ui-serve-demo runId=<run id>
TOOL_CALL_START <id> Write
RUN_FINISHED interrupt
approval requested: Write (interrupt <id>), answering resolved
RUN_STARTED threadId=ag-ui-serve-demo runId=<resumed run id>
TOOL_CALL_RESULT <id> {"tool_name":"Write"... success=true ...}
TEXT_MESSAGE_CONTENT The marker file is written.
RUN_FINISHED success
```

## Which event is which

Every row maps into [AG-UI Output Format](../../docs/ag-ui-output.md):

| Printed line | Meaning |
| --- | --- |
| `RUN_STARTED` | The run opens. `threadId` is the session id and `runId` is fresh per run |
| `TEXT_MESSAGE_CONTENT` | One streamed chunk of an assistant message. The role rode the skipped `TEXT_MESSAGE_START` |
| `TOOL_CALL_START` | A tool call opens. The program shows the call id and name |
| `TOOL_CALL_RESULT` | The raw JSON execution result, with the legacy `"Result of tool call:"` prefix gone |
| `RUN_FINISHED success` | The run's terminal event, carrying `usage` and `result` |
| `RUN_FINISHED interrupt` | The run suspends on a tool approval. `resolved` answers it, `cancelled` declines |
| `-- next frame --` | Serve worker vocabulary: app frames on stdin carry run inputs, events come back per frame |

| Printed line | Documented in |
| --- | --- |
| `RUN_STARTED`, `TEXT_MESSAGE_*`, `TOOL_CALL_*`, `RUN_FINISHED success` | [Event mapping](../../docs/ag-ui-output.md#event-mapping) |
| Resuming a thread by passing `threadId` back as `--session-id` | [Subprocess hosts](../../docs/ag-ui-output.md#subprocess-hosts) |
| `RUN_FINISHED interrupt` and the resume answer | [Interrupts](../../docs/ag-ui-output.md#interrupts) |
| `-- next frame --` and the frames around it | [Serve worker](../../docs/ag-ui-output.md#serve-worker) |
| The `usage` fields on `RUN_FINISHED` | [Usage and result](../../docs/ag-ui-output.md#usage-and-result) |

The program deliberately skips the events it does not print: `STATE_SNAPSHOT`/`STATE_DELTA` (the run's state, see
[State](../../docs/ag-ui-output.md#state)), `ACTIVITY_SNAPSHOT` (progress such as tool status, see
[Activity](../../docs/ag-ui-output.md#activity)), `MESSAGES_SNAPSHOT`, and the `_START`/`_END`/`_ARGS` brackets. Drop
the filter in `describe()` to see every line.

## Without Task

The Taskfile only sets four environment variables. Set them yourself to run `infer` directly from this directory:

```bash
export INFER_GATEWAY_MOCK=true
export INFER_GATEWAY_MOCK_SCENARIOS="$PWD/scenarios.yaml"
export INFER_AGENT_MODEL=mock/openai/gpt-4o
export INFER_STORAGE_ENABLED=false

infer headless --format ag-ui --no-save "stream a run with a tool call" | go run . stream
go run . serve
go run . serve --approval
```

`serve` spawns the `infer` on your `PATH`. Set `INFER=../../infer` to point it at a local build.

## Use a real model

Drop `INFER_GATEWAY_MOCK`, `INFER_GATEWAY_MOCK_SCENARIOS` and `INFER_AGENT_MODEL` to talk to a real model through the
gateway - the events, the frames and the approval dance are unchanged.
