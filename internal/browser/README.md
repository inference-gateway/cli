# browser

**What** - the browser bounded context: navigating, reading, clicking, typing, taking screenshots and listing tabs in a real browser.
**Why** - browser automation pulls in a heavy engine and a foreign event loop, so it sits behind an anti-corruption
layer instead of leaking into the agent.
**How** - `domain/` defines the browser port, and `infrastructure/` implements it with Playwright or with a thin
`ExtensionDriver` adapter over the opentask extension.

## How it plugs in

- `NewTools(...)` builds the six `Browser*` tools from the manifests in `tools/`, and the container registers them into the tools registry.
- Swapping Playwright for the extension adapter, or adding another engine, must not change the tool contract the agent calls.
- `ExtensionDriver` builds `browser_command` frames, owns the per-action timeout and maps `browser_result`
  payloads to `BrowserToolResult`. Its transport is the injected `ExtensionRequest`.
- This context owns the browser wire contract and uses `internal/protocols/agui` as its transport. agui knows
  nothing about browsers, so the frame names, the client kinds, the handshake and the accepted extension origins
  live in `infrastructure/extension_wire.go`.
- `ExtensionRelay` is the host end. Behind the binding `infer daemon` hosts, it keeps the one extension
  connection, replaces it when the extension reconnects, and drives every `browser_command` through it one at a
  time, because one browser serves every source. It also reports the extension's state to every other connection
  as `browser_extension_status` frames, once when a connection joins and again on attach and detach.
- `ExtensionClient` is the client end and the `ExtensionRequest` of `infer chat` and a standalone
  `infer headless`. It dials the binding as a `browser` client after its `EnsureHost` hook made a host listen. The
  container passes `daemon.EnsureRunning`, which starts the daemon on demand.
- A headless `--serve` worker reroutes the seam through `RouteBrowserRequests` to a stdio relay: `browser_command`
  lines go out on stdout and `browser_result` lines come back on stdin, so the worker reaches the extension through its host
  (the daemon, which routes and serializes) and binds no port.

## Related

- [Daemon Binding Protocol](../../docs/browser-extension-protocol.md)
- [Tools Reference](../../docs/tools-reference.md#browser-tools)
