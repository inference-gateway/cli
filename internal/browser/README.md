# browser

**What** - the browser bounded context: navigating, reading, clicking, typing, taking screenshots and listing tabs in a real browser.
**Why** - browser automation pulls in a heavy engine and a foreign event loop, so it sits behind an anti-corruption
layer instead of leaking into the agent.
**How** - `domain/` defines the browser port, and `infrastructure/` implements it with Playwright or with a thin
`ExtensionDriver` adapter over the opentask extension bridge.

## How it plugs in

- `NewTools(...)` builds the six `Browser*` tools from the manifests in `tools/`, and the container registers them into the tools registry.
- Swapping Playwright for the extension adapter, or adding another engine, must not change the tool contract the agent calls.
- `ExtensionDriver` builds `browser_command` frames, owns the per-action timeout and maps `browser_result`
  payloads to `BrowserToolResult`. The container injects the daemon bridge client's `Request` function (declared here
  as `ExtensionRequest`), so this context holds no socket: the WebSocket, handshake, the binding and the panel units live in
  `internal/protocols/agui/`. Only `infer daemon` hosts the binding; the client starts the daemon on demand.
- A headless `--serve` worker reroutes the seam through `RouteBrowserRequests` to a stdio relay: `browser_command`
  lines go out on stdout and `browser_result` lines come back on stdin, so the worker reaches the extension through its host
  (the daemon, which routes and serializes) and binds no port.

## Related

- [Browser Extension Bridge Protocol](../../docs/browser-extension-protocol.md)
- [Tools Reference](../../docs/tools-reference.md#browser-tools)
