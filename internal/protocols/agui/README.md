# agui

**What** - the AG-UI publisher and the opentask browser-extension bridge: rendering agent chat events
on the AG-UI protocol, plus the one localhost WebSocket the opentask extension dials into.
**Why** - external orchestrators and clients need a stable published event schema that is not the CLI's
internal chat-event type, and the extension socket is a protocol endpoint, not a browser adapter.
**How** - `Render` maps agent events to newline-delimited AG-UI JSON. `ExtensionBridge` owns the listener,
the `Origin` check, the `browser_hello` token handshake, one active connection with replacement, pings,
the `/artifacts/` route, and `Request`, the command/result RPC the browser driver calls.

## How it plugs in

- `infer headless --format ag-ui` selects `Render`. The extension bridge's chat mirror reuses it for the
  panel's event stream.
- The container builds `ExtensionBridge` (see `Deps`) and injects its `Request` method into the browser
  capability's `ExtensionDriver`, so the browser context never imports this package.
- Panel frames route to small units, one per frame family: `extension_chat.go` (chat mirror, approvals),
  `extension_panel.go` (conversations, history, skills, models, modes) and `extension_tools.go` (tool
  requests). `ExtensionBridge` only routes; `extension_driver.go` on the browser side is the consumer.
- There is no `domain/` subpackage: the only model it owns is the published event mapping and the
  extension wire frames.

## Related

- [AG-UI Output Format](../../../docs/ag-ui-output.md)
- [Commands Reference](../../../docs/commands-reference.md#infer-headless)
- [Browser Extension Bridge Protocol](../../../docs/browser-extension-protocol.md)
