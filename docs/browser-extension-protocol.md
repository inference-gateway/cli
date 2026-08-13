# Browser Extension Bridge Protocol

The CLI can drive the **user's real browser** through the
[opentask](https://github.com/inference-gateway/opentask) extension instead of
a Playwright-launched one, and mirror the chat conversation into the
extension. This document is the wire contract the extension implements.

## Transport

- The CLI listens on `ws://127.0.0.1:<port>/ws` (default port `52789`,
  `browser_use.yaml` → `extension.port`). The extension dials in — MV3
  service workers cannot listen.
- Every frame is a single JSON text message with a `type` discriminator.
  Unknown `type` values MUST be ignored (forward compatibility).
- Auth is a shared token (`extension.token` in `browser_use.yaml`, copied
  into the extension options). Browser WebSocket clients cannot set headers,
  so the token rides in the first message.
- One extension connection at a time; a newly authenticated connection
  replaces the previous one (service workers restart at will). The CLI sends
  WS pings every ~20s to keep the service worker alive.
- Only `chrome-extension://`, `moz-extension://`, `safari-web-extension://`
  (or absent) `Origin` headers are accepted.

Enable with:

```yaml
# ~/.infer/browser_use.yaml
enabled: true
backend: extension
extension:
  port: 52789
  token: <shared secret; infer init seeds one>
```

## Handshake

Extension → CLI, first frame, within 5 seconds of connecting:

```json
{"type": "browser_hello", "token": "<shared secret>", "extension_version": "1.9.2"}
```

CLI → extension on success (on failure the socket is closed):

```json
{"type": "browser_hello_ack", "protocol_version": 1}
```

Immediately after the ack the CLI sends a conversation snapshot (see below).

## Browser commands (CLI → extension)

One shape, four actions; only the fields relevant to the action are set.
`timeout_ms` is the per-action budget the extension must enforce.

```json
{"type": "browser_command", "id": "<uuid>", "action": "navigate", "url": "https://example.com", "timeout_ms": 30000}
{"type": "browser_command", "id": "<uuid>", "action": "click",    "selector": "button.submit", "timeout_ms": 30000}
{"type": "browser_command", "id": "<uuid>", "action": "type",     "selector": "input[name=q]", "text": "hello", "press_enter": true, "timeout_ms": 30000}
{"type": "browser_command", "id": "<uuid>", "action": "read",     "selector": "", "timeout_ms": 30000}
```

- `navigate`: open the URL in the controlled tab (`chrome.tabs.update`, or
  `chrome.tabs.create` when none exists). The extension chooses/owns the
  controlled tab; the protocol has no tab id yet (additive later).
- `click`: `document.querySelector(selector).click()` semantics.
- `type`: replace the element's value with `text`, dispatch `input`/`change`,
  then a keyboard Enter when `press_enter` is true.
- `read`: `innerText` of the selector (empty selector means `body`).

Extension → CLI, exactly one result per command id:

```json
{"type": "browser_result", "id": "<uuid>", "url": "https://example.com/", "title": "Example", "content": "...", "events": [], "error": ""}
```

- `error != ""` means the command failed; other fields may be empty.
- `content` is only meaningful for `read`. `events` carries optional
  browser-initiated notices (console lines etc.) and may always be empty.
- `url`/`title` reflect the controlled tab after the action.

## Conversation sync

Sent by the CLI right after the handshake:

```json
{"type": "conversation_snapshot", "messages": [{"role": "user", "content": "..."}, ...]}
```

`messages` are the gateway SDK message objects of the current conversation.

Then the CLI streams live chat activity, one frame per
[AG-UI](https://docs.ag-ui.com/) event (same encoding as
`infer headless --output ag-ui`, see `docs/ag-ui-output.md`):

```json
{"type": "chat_event", "event": {"type": "TEXT_MESSAGE_CONTENT", "delta": "..."}}
```

The extension renders these however it likes; ignoring event types it does
not understand is expected.

Extension → CLI, to send a user message into the conversation (queued if the
agent is busy, exactly like typing in the TUI):

```json
{"type": "user_message", "content": "please also check the docs page"}
```

Tool approval prompts are **not** mirrored in protocol v1 — approvals stay in
the terminal. An `approval` flow is reserved for a future protocol version.

## Extension-side checklist

- WS client in the background service worker; port + token from the options
  page storage; reconnect with backoff.
- `tabs` + `scripting` permissions and matching host permissions for the
  controlled tab.
- Known ceiling: `chrome.scripting`-synthesized clicks/keys are untrusted
  events some sites ignore; the upgrade path is `chrome.debugger` (CDP
  `Input.dispatch*`), which changes extension permissions, not this protocol.
