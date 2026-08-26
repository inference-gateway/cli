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
{"type": "browser_hello_ack"}
```

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
- `screenshot`: capture the visible controlled tab
  (`chrome.tabs.captureVisibleTab`) and return it as base64 in the result's
  `image` field. Passwords render masked by the browser, so no extra redaction
  is required.
- `tabs`: enumerate the open tabs (`chrome.tabs.query`) and return them in
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

The panel drives which conversation it shows. The CLI does not auto-send a
snapshot on connect — the panel lists and resumes conversations explicitly.

Extension → CLI, list the user's stored conversations (the same ones `infer`
resumes from, under `.infer/conversations`):

```json
{"type": "list_conversations"}
```

CLI → extension, newest-first (sorted by `updated_at` descending):

```json
{"type": "conversations", "conversations": [{"id": "<uuid>", "title": "...", "updated_at": "2026-08-16T12:00:00Z", "message_count": 12}]}
```

- `title` is the conversation's title (an auto-derived first-message preview
  until a better one is generated); `updated_at` is RFC 3339; `message_count`
  is the number of stored messages.
- The array is empty when the CLI runs without conversation persistence
  (`storage.enabled: false`).

Extension → CLI, resume one — makes it the active conversation:

```json
{"type": "resume_conversation", "id": "<uuid>"}
```

CLI → extension, the resumed conversation's history:

```json
{"type": "conversation_snapshot", "messages": [{"role": "user", "content": "..."}, ...]}
```

- `messages` are the gateway SDK message objects of the resumed conversation.
  Assistant entries keep their `tool_calls` (id, function name, arguments) and
  tool entries their `tool_call_id`, so the panel can rebuild tool rows.
- `tool_results` maps `tool_call_id` to whether that execution succeeded, for
  entries the CLI has an execution record for.
- An unknown or empty `id` is ignored (no snapshot is sent).

After the snapshot the CLI streams live chat activity for the active
conversation, one frame per
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

Extension → CLI, to stop the turn currently streaming (same as `esc` in the
TUI; a no-op when nothing is running):

```json
{"type": "interrupt"}
```

The agent then emits its usual cancelled completion; the panel sees the stream
end and no separate acknowledgement frame.

## Skills

The panel offers a "/" autocomplete of the agent's skills. It asks the CLI for
the merged, scope-tagged list the CLI already resolves (project, `.agents`,
user, plugin, catalog), so the menu mirrors what the TUI offers.

Extension → CLI, list the available skills:

```json
{"type": "list_skills"}
```

CLI → extension, the discovered skills (empty when skills are unavailable):

```json
{"type": "skills", "skills": [{"name": "tmux", "description": "...", "scope": "user"}]}
```

- `name` is the qualified skill name (`pluginName:skillName` for plugin skills).
- `scope` is one of `project`, `agents`, `user`, `plugin`, `catalog`; name
  conflicts are already resolved by precedence, so each name appears once.
  Unknown scopes are ignored by the extension.

## Models

The extension offers model pickers (e.g. the default model when installing the
task workflow). It asks the CLI for the models the CLI itself is configured
with, so the extension never hardcodes a model list.

Extension → CLI:

```json
{"type": "list_models"}
```

CLI → extension, the configured model ids (empty when unavailable):

```json
{"type": "models", "models": ["anthropic/claude-sonnet-4-5", "ollama_cloud/deepseek-v4"], "current": "anthropic/claude-sonnet-4-5"}
```

- Each entry is a `provider/model` id exactly as the CLI would accept it.
- The first entry is the CLI's default model.

Extension → CLI, switch the CLI's active model (same effect as `/model` in the
TUI). The CLI answers with a fresh `models` frame; `current` shows whether the
switch took effect:

```json
{"type": "select_model", "model": "openai/gpt-4o"}
```

## Agent mode

The panel can toggle the CLI's agent mode (the same shared state as the TUI's
shift+tab cycle; it also governs `tool_request` approvals). Modes travel as
their allowlist keys: `standard`, `plan`, `auto`.

CLI → extension, sent on hello and after every `set_mode`:

```json
{"type": "mode", "mode": "standard"}
```

Extension → CLI, switch the mode. Unknown values are ignored; the CLI answers
with a fresh `mode` frame either way:

```json
{"type": "set_mode", "mode": "auto"}
```

## Artifacts (generated images)

Chat text can reference files the agent saved under the artifacts dir
(`~/.infer/artifacts/<...>`, e.g. `ImageGeneration` output). An MV3 extension
cannot load a local file path in `<img>`, so alongside `/ws` the CLI serves that
directory read-only over HTTP:

```text
GET http://127.0.0.1:<port>/artifacts/<relative-path>
```

The extension rewrites a markdown image whose URL contains `/.infer/artifacts/`
to this route (stripping the prefix through and including `artifacts/`) and
renders it inline. The route is loopback-only and unauthenticated (the artifacts
are the user's own generated files); path traversal is blocked by `http.Dir`.

## Tool approvals

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

## Tool calls (extension → CLI)

The extension can invoke any of the CLI's tools (see `docs/tools-reference.md`)
through the bridge. The CLI routes each request through its **normal tool
execution pipeline** — the same permissions, allowlists, and approval flow as a
tool call made by the agent. The protocol stays generic: new capabilities need
no new frame types.

Extension → CLI:

```json
{"type": "tool_request", "id": "<uuid>", "tool_name": "Bash", "tool_args": "{\"command\":\"gh api user\"}"}
```

CLI → extension, exactly one result per request id:

```json
{"type": "tool_result", "id": "<uuid>", "success": true, "output": "...", "error": ""}
```

- `tool_name`/`tool_args` use the same vocabulary as `approval_request`:
  `tool_args` is the raw tool-arguments JSON string.
- The approval flow applies: an `approval_request` (with its own `request_id`)
  may precede the result; a denial produces `{"success": false, "error": ...}`
  — never a dropped id.
- `success: false` / `error != ""` means failure. For `Bash`, `output` is the
  combined stdout/stderr; a non-zero exit sets `success: false` with `output`
  still populated.
- An unknown `tool_name` yields one failed result, not a closed socket.
- The extension enforces its own timeout and MUST ignore a late `tool_result`
  with an unknown id (same rule as stale `approval_resolved`). If the socket
  drops before the result, the CLI drops it — no queuing or replay; the
  extension fails its pending calls on disconnect, and a reconnect does not
  resurrect old ids.
- Primary consumer: the extension performs all GitHub API access as `Bash`
  requests running `gh api ...` with the user's own `gh` auth. Allowlisting
  `gh api` in the CLI's bash-allow configuration avoids an approval prompt per
  call.

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
