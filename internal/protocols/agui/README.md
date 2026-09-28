# agui

**What** - the AG-UI publisher: translating internal agent events into the AG-UI protocol event stream for headless runs.
**Why** - external orchestrators and desktop clients need a stable event schema that is not the CLI's own internal chat-event type.
**How** - the agent emits its events, this package maps them to AG-UI and writes the stream; the SDK stays here.

## What it owns

- Translating agent, tool and run events into AG-UI event types.
- The `RUN_FINISHED` result payload, including the final assistant output.
- The `--output agui` path of `infer headless` and the desktop sidecar wiring.

## Why it is separate

Unlike the A2A and MCP contexts, AG-UI is outbound only: nothing is translated back into the agent.
It therefore has no `domain/` subpackage, only the translation and the transport.

## How it plugs in

- The AG-UI SDK (`github.com/ag-ui-protocol/ag-ui`) is confined to this package by depguard.
- `infer headless --output agui` selects it; the event mapping and the run result shape are documented below.

## Related

- [AG-UI Output Format](../../../docs/ag-ui-output.md)
- [Commands Reference](../../../docs/commands-reference.md)
