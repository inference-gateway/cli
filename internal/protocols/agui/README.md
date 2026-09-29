# agui

**What** - the AG-UI protocol: the run encoder that renders agent chat events as AG-UI events, a localhost
WebSocket binding with both of its ends, and the panel units a session worker answers panel requests with.
**Why** - external orchestrators and clients need a stable published event schema that is not the CLI's
internal chat-event type, carried over a transport that does not care who connects.
**How** - `RunEncoder` maps agent events to newline-delimited AG-UI JSON, one run per turn. `Binding` owns the
listener, the `Origin` check, the token handshake, the connections and their pings, and hands every frame to
its `Handler`. `Dial` is the client end and hands the frames it receives to a `Handler` too. `Panel` answers
the panel frames on a worker's stdout.

## What it does not know

This package is general purpose. It names no client kind, no context and no host, and depguard
(`agui-names-no-context`) keeps it from importing one. The contexts import it:

- A `Conn` carries the client kind its hello declared as an opaque string. What a kind means is the
  handler's business.
- The handshake frame names and the accepted origins come in through `BindingConfig` and `DialConfig`.
- A context publishes its own CUSTOM events through the `Publish` seam of the run encoder, so the encoder
  maps only the agent's own chat events.

## How it plugs in

- `infer headless --format ag-ui` drives one `RunEncoder` for its run, and `infer headless --serve` drives one
  per turn. Both pass the `Publish` functions of the contexts whose events the stream carries.
- `cmd/daemon` hosts the one `Binding` and puts its consumers behind the handler: the browser context's
  extension relay and the `sessions` thread registry (see `internal/browser` and `internal/sessions`).
- `internal/browser` dials the binding through `Dial` to reach the extension from `infer chat` and a
  standalone `infer headless`.
- `infer headless --serve` builds a `Panel` over its own container and hands it every stdin line first.
  `panel.go` holds the conversation, history, skill, model and mode units, and `panel_tools.go` runs
  `tool_request`s through the approval policy.
- There is no `domain/` subpackage: the only model it owns is the published event mapping and the wire
  frames.

## Related

- [AG-UI Output Format](../../../docs/ag-ui-output.md)
- [Commands Reference](../../../docs/commands-reference.md#infer-headless)
- [Browser Extension Bridge Protocol](../../../docs/browser-extension-protocol.md)
