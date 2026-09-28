# agui

**What** - the AG-UI publisher: rendering a headless run's chat events as the AG-UI protocol event stream.
**Why** - external orchestrators and desktop clients need a stable, published event schema that is not the CLI's internal chat-event type.
**How** - `Render` maps agent events to AG-UI events and writes them as newline-delimited JSON; approval and question
answers flow back through the channels it is given.

## How it plugs in

- `infer headless --output agui` selects it. The browser extension bridge reuses it for its own event stream.
- A run emits one `RUN_STARTED`, per-turn deltas and tool calls, then one `RUN_FINISHED` carrying the session
  stats (tokens, cost, tool calls) or one `RUN_ERROR`.
- There is no `domain/` subpackage: the only model it owns is the published event mapping.

## Related

- [AG-UI Output Format](../../../docs/ag-ui-output.md)
- [Commands Reference](../../../docs/commands-reference.md#infer-headless)
