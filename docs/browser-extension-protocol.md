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
{"type": "browser_hello_ack", "protocol_version": 3}
```

Immediately after the ack the CLI sends a conversation snapshot (see below).

## Browser commands (CLI → extension)

One shape, six actions; only the fields relevant to the action are set.
`timeout_ms` is the per-action budget the extension must enforce.

```json
{"type": "browser_command", "id": "<uuid>", "action": "navigate",   "url": "https://example.com", "timeout_ms": 30000}
{"type": "browser_command", "id": "<uuid>", "action": "click",      "selector": "button.submit", "timeout_ms": 30000}
{"type": "browser_command", "id": "<uuid>", "action": "type",       "selector": "input[name=q]", "text": "hello", "press_enter": true, "timeout_ms": 30000}
{"type": "browser_command", "id": "<uuid>", "action": "read",       "selector": "", "timeout_ms": 30000}
{"type": "browser_command", "id": "<uuid>", "action": "screenshot", "timeout_ms": 30000}
{"type": "browser_command", "id": "<uuid>", "action": "tabs",       "timeout_ms": 30000}
```

- `navigate`: open the URL in the controlled tab (`chrome.tabs.update`, or
  `chrome.tabs.create` when none exists). The extension chooses/owns the
  controlled tab; the protocol has no tab id yet (additive later).
- `click`: `document.querySelector(selector).click()` semantics.
- `type`: replace the element's value with `text`, dispatch `input`/`change`,
  then a keyboard Enter when `press_enter` is true.
- `read`: `innerText` of the selector (empty selector means `body`). **Must
  redact secrets:** never return the `value` of `<input type="password">`,
  inputs whose `autocomplete` is `current-password`/`new-password`/
  `one-time-code`, or inputs whose name/id/aria-label matches
  `/pass|secret|token|otp|cvc|card/i` — substitute `"[redacted]"`. `innerText`
  already excludes input values; this rule binds any richer extraction.
- `screenshot` (v3): capture the visible controlled tab
  (`chrome.tabs.captureVisibleTab`) and return it as base64 in the result's
  `image` field. Passwords render masked by the browser, so no extra redaction
  is required.
- `tabs` (v3): enumerate the open tabs (`chrome.tabs.query`) and return them in
  the result's `tabs` array, flagging the controlled/active one.

Extension → CLI, exactly one result per command id:

```json
{"type": "browser_result", "id": "<uuid>", "url": "https://example.com/", "title": "Example", "content": "...", "events": [], "error": ""}
{"type": "browser_result", "id": "<uuid>", "image": "<base64>", "image_mime_type": "image/png", "url": "...", "title": "..."}
{"type": "browser_result", "id": "<uuid>", "tabs": [{"index": 0, "url": "...", "title": "...", "active": true}]}
```

- `error != ""` means the command failed; other fields may be empty.
- `content` is only meaningful for `read`; `image`/`image_mime_type` for
  `screenshot`; `tabs` for `tabs`. `events` carries optional browser-initiated
  notices (console lines etc.) and may always be empty.
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

## Tool approvals (protocol v2)

When a tool call needs the user's approval, the CLI sends an approval request
instead of mirroring it as a chat line. The extension shows Approve/Deny and
sends the decision back. The same decision can still be made in the terminal —
whichever answers first wins; the loser is a harmless no-op.

CLI → extension, one per pending tool call:

```json
{"type": "approval_request", "request_id": "<uuid>", "tool_name": "Bash", "tool_args": "{\"command\":\"rm -rf ...\"}"}
```

- `tool_args` is the raw tool-call arguments JSON string (may be empty).

Extension → CLI, the user's decision:

```json
{"type": "approval_response", "request_id": "<uuid>", "action": "approve"}
```

- `action` is `"approve"` or `"reject"`. Any other value (including unknown
  future actions) is treated as `reject`, failing safe.

CLI → extension, when the request is no longer pending (answered in the panel
**or** in the terminal) so the extension can clear its prompt:

```json
{"type": "approval_resolved", "request_id": "<uuid>"}
```

- A `request_id` the extension does not recognize (already cleared, or never
  seen) MUST be ignored. Duplicate `approval_resolved` for the same id is fine.
- An `approval_response` for an unknown/already-answered `request_id` is ignored
  by the CLI.

## Extension-side checklist

- WS client in the background service worker; port + token from the options
  page storage; reconnect with backoff.
- `tabs` + `scripting` permissions and matching host permissions for the
  controlled tab. `screenshot` additionally needs `activeTab`/host permission
  for `chrome.tabs.captureVisibleTab`.
- `read` must redact secret input values before returning (see the `read`
  action above); `screenshot` must not click-to-reveal masked fields.
- Known ceiling: `chrome.scripting`-synthesized clicks/keys are untrusted
  events some sites ignore, and there is no coordinate-click action for the same
  reason; the upgrade path is `chrome.debugger` (CDP `Input.dispatch*`), which
  changes extension permissions, not this protocol.
