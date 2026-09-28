# browser

**What** - the browser bounded context: navigating, reading, clicking, typing, taking screenshots and listing tabs in a real browser.
**Why** - browser automation pulls in a heavy engine and a foreign event loop, so it sits behind an anti-corruption
layer instead of leaking into the agent.
**How** - `domain/` defines the browser port, and `infrastructure/` implements it with Playwright or through the companion extension bridge.

## How it plugs in

- `NewTools(...)` builds the six `Browser*` tools from the manifests in `tools/`, and the container registers them into the tools registry.
- Swapping Playwright for the extension bridge, or adding another engine, must not change the tool contract the agent calls.
- The extension's side panel (chat mirror, models, modes, conversations, history, skills, tool requests) shares the bridge's
  one WebSocket with the browser commands, so its frame handlers live beside `ExtensionBridge` in `infrastructure/`, one small
  unit per frame family. `ExtensionBridge` owns the connection and only routes frames to them.

## Related

- [Browser Extension Bridge Protocol](../../docs/browser-extension-protocol.md)
- [Tools Reference](../../docs/tools-reference.md#browser-tools)
