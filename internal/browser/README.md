# browser

**What** - the browser bounded context: navigating, reading, clicking, typing, taking screenshots and listing tabs in a real browser.
**Why** - browser automation pulls in a heavy engine and a foreign event loop, so it sits behind an anti-corruption
layer instead of leaking into the agent.
**How** - `domain/` defines the browser port, and `infrastructure/` implements it with Playwright or through the companion extension bridge.

## How it plugs in

- `NewTools(...)` builds the six `Browser*` tools from the manifests in `tools/`, and the container registers them into the tools registry.
- Swapping Playwright for the extension bridge, or adding another engine, must not change the tool contract the agent calls.
- Configure it in `browser_use.yaml` (or `INFER_BROWSER_USE_*`).

## Related

- [Browser Extension Bridge Protocol](../../docs/browser-extension-protocol.md)
- [Tools Reference](../../docs/tools-reference.md#browser-tools)
