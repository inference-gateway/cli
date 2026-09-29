# agui

**What** - the AG-UI publisher and the localhost AG-UI WebSocket binding: rendering agent chat events on the
AG-UI protocol, the one socket the opentask extension and the desktop app dial into, and the panel units a
session worker answers panel requests with.
**Why** - external orchestrators and clients need a stable published event schema that is not the CLI's
internal chat-event type, and the socket is a protocol endpoint, not a browser adapter.
**How** - `RunEncoder` maps agent events to newline-delimited AG-UI JSON, one run per turn. `ExtensionBridge`
owns the listener, the `Origin` check, the `browser_hello` token handshake, one extension connection with
replacement plus any number of desktop connections, pings, the `/artifacts/` route, and `Request`, the
command/result RPC the browser driver calls. `Panel` answers the panel frames on a worker's stdout.

## How it plugs in

- `infer headless --format ag-ui` drives one `RunEncoder` for its run, and `infer headless --serve` drives one per
  turn.
- In `infer daemon` the bridge gets a `sessions` `Threads` port. Each connection is a sessions `Client`, and
  every frame except `browser_result` is relayed to its thread's worker (see `internal/sessions`).
- In `infer chat` the bridge has no `Threads`. It only carries browser commands for the extension browser
  backend. The container injects its `Request` method into the browser capability's `ExtensionDriver`, so
  the browser context never imports this package.
- `infer headless --serve` builds a `Panel` over its own container and hands it every stdin line first.
  `extension_panel.go` holds the conversation, history, skill, model and mode units, and `extension_tools.go`
  runs `tool_request`s through the approval policy.
- There is no `domain/` subpackage: the only model it owns is the published event mapping and the wire
  frames.

## Related

- [AG-UI Output Format](../../../docs/ag-ui-output.md)
- [Commands Reference](../../../docs/commands-reference.md#infer-headless)
- [Browser Extension Bridge Protocol](../../../docs/browser-extension-protocol.md)
- [sessions](../../sessions/README.md)
