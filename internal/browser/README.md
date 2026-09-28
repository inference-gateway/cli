# browser

**What** - the browser bounded context: navigating, reading, clicking, typing, taking screenshots and managing tabs in a real browser.
**Why** - browser automation pulls in a heavy engine and a foreign event loop, so it is kept behind an
anti-corruption layer instead of leaking Playwright into the agent.
**How** - `domain/` defines the browser contract, and `infrastructure/` implements it with Playwright or through the companion extension bridge.

## What it owns

- `domain/browser.go` - the browser port (navigate, read, click, type, screenshot, tabs).
- `base.go`, `browser_*.go` - the tool handlers built on that port.
- `tools.go`, `tool_manifests.go` - building and describing the browser tools.
- `infrastructure/playwright.go` - the Playwright implementation (the only place Playwright appears).
- `infrastructure/extension_bridge.go` - driving a user's own browser through the extension.
- `tools/*.yaml` - one manifest per tool: `BrowserNavigate`, `BrowserRead`, `BrowserClick`, `BrowserType`, `BrowserScreenshot`, `BrowserTabs`.

## Why it is separate

The engine is an implementation detail. Swapping Playwright for the extension bridge, or adding
another engine, must not change the tool contract the agent calls.

## How it plugs in

- `NewTools(...)` builds the browser tools from the manifests and the container registers them into the tools registry.
- Configure it in `browser_use.yaml` (or `INFER_BROWSER_USE_*`). The extension handshake and message shapes are documented in the protocol page below.
- Depguard keeps Playwright inside this package.

## Related

- [Browser Extension Bridge Protocol](../../docs/browser-extension-protocol.md)
- [Tools Reference](../../docs/tools-reference.md#browser-tools)
