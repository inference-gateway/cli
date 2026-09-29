# Browser Extension Bridge Protocol

The opentask [extension](https://github.com/inference-gateway/opentask) and the
desktop app talk to the agent through one localhost AG-UI WebSocket binding.
`infer daemon` hosts it: each thread (a project dir plus a conversation id) runs
in its own session worker, and the binding relays the worker's AG-UI events to
the thread's clients as bare frames. The same socket also lets the CLI drive the
**user's real browser** through the extension instead of a Playwright-launched
one. This document is the wire contract the clients implement.

## Transport

- `infer daemon` listens on `ws://127.0.0.1:<port>/ws` (default port `52789`,
  `browser_use.yaml` → `extension.port`) when `browser_use` is enabled with
  `backend: extension`. Clients dial in — MV3 service workers cannot listen.
- Only the daemon binds the port. `infer chat` and a standalone `infer headless`
  reach the browser as browser clients through the daemon (`client: "browser"` in
  the hello) and start `infer daemon` when nothing is listening. A session
  worker's `browser_command` stdout lines are routed to the extension connection
  by the daemon, and each `browser_result` goes back to that worker by `id`;
  commands serialize across threads because one browser serves them.
- Every frame is a single JSON text message with a `type` discriminator.
  AG-UI events use an uppercase `type` and app frames a lowercase one. Unknown
  `type` values MUST be ignored (forward compatibility).
- Auth is a shared token (`extension.token` in `browser_use.yaml`, copied
  into the extension options). Browser WebSocket clients cannot set headers,
  so the token rides in the first message.
- One extension connection at a time; a newly authenticated extension
  replaces the previous one (service workers restart at will). Any number of
  desktop connections can be open next to it. The CLI sends WS pings every ~20s
  to keep the service worker alive.
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

Client → CLI, first frame, within 5 seconds of connecting:

```json
{"type": "browser_hello", "token": "<shared secret>", "client": "extension", "protocol_version": 1, "extension_version": "1.9.2"}
```

- `client` is `extension` or `desktop`. An absent or unknown value counts as
  `extension`.
- The handshake is lenient: any hello with a valid token is accepted. A hello
  without `protocol_version` is logged as a warning.

CLI → client on success (on failure the socket is closed):

```json
{"type": "browser_hello_ack", "protocol_version": 1}
```

A client that expects another `protocol_version` should show an "update" state
instead of refusing to work.

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

## Threads

A connection follows at most one thread at a time, because AG-UI events carry
no thread id outside `RUN_STARTED` / `RUN_FINISHED`. The desktop app opens one
connection per thread. The CLI does not auto-send anything on connect.

Client → CLI, start a new conversation in a project, or resume a stored one.
Both make the connection follow that thread:

```json
{"type": "new_session", "project_dir": "/abs/path/to/project", "model": "openai/gpt-4o", "mode": "standard"}
{"type": "resume_conversation", "project_dir": "/abs/path/to/project", "id": "<conversation id>"}
```

- `project_dir` is required and absolute. The thread's worker runs with it as
  its working dir, so the project's `.infer/` config, storage, skills and
  sandbox apply.
- Optional thread options override the project's config for this thread's
  worker. They apply when the worker launches, so they have no effect on a
  thread whose worker is already running:
  `model`, `mode` (`standard`, `plan`, `auto`, `auto-with-judge`),
  `system_prompt`, `custom_instructions`, `sandbox_directories` (array of
  paths) and `max_turns`.
- `resume_conversation` requires `id`. A worker that resumes a long-idle
  conversation can roll it over to a new id, so resume with the last `threadId`
  you saw.

CLI → client, the thread's history as an AG-UI snapshot (empty for a new
session):

```json
{"type": "MESSAGES_SNAPSHOT", "messages": [{"id": "...", "role": "user", "content": "..."}]}
```

After that the client receives every event the thread's worker writes, one
bare [AG-UI](https://docs.ag-ui.com/) event per frame, in the same encoding as
`infer headless --format ag-ui` (see `docs/ag-ui-output.md`). Every client
following the thread receives the same frames. Each agent turn is one run:
`RUN_STARTED` with `threadId` set to the conversation id, then exactly one
`RUN_FINISHED` or `RUN_ERROR`. A stopped turn ends with `RUN_FINISHED` and
outcome `{"type": "cancelled"}`.

Client → CLI, send a user message into the thread (queued if the agent is busy,
exactly like typing in the TUI):

```json
{
  "type": "user_message",
  "content": "please also check the docs page",
  "attachments": [{"filename": "shot.png", "mime_type": "image/png", "data": "<base64>"}]
}
```

- `attachments` is optional. Each file is saved to the project's tmp dir. PNG,
  JPEG, GIF and WebP images reach the model as image parts, and other files as
  a note naming the saved path. Files over 10 MiB are skipped.

Client → CLI, stop the turn currently streaming (a no-op when nothing runs):

```json
{"type": "interrupt"}
```

`user_question_response` and `computer_use_control` frames are forwarded to the
worker unchanged, with the shapes documented in `docs/ag-ui-output.md`.

When the CLI cannot route a frame (a relative `project_dir`, a `user_message`
before any `new_session`, a worker that fails to start), it answers the sender
with a `RUN_ERROR` that has no `runId`. When a thread's worker exits mid-turn,
every client of the thread gets a `RUN_ERROR` carrying the open run's `runId`.
The next `user_message` starts a new worker for the thread.

## Conversations

Client → CLI, list a project's stored conversations (the same ones `infer`
resumes from, under `~/.infer/projects/<project-slug>/conversations`):

```json
{"type": "list_conversations", "project_dir": "/abs/path/to/project"}
```

CLI → client, newest-first (sorted by `updated_at` descending):

```json
{"type": "conversations", "conversations": [{"id": "<uuid>", "title": "...", "updated_at": "2026-08-16T12:00:00Z", "message_count": 12}]}
```

- `title` is the conversation's title (an auto-derived first-message preview
  until a better one is generated); `updated_at` is RFC 3339; `message_count`
  is the number of stored messages.
- The array is empty when the project runs without conversation persistence
  (`storage.enabled: false`).

### Project-scoped requests

`list_conversations`, `list_history`, `list_skills`, `list_models`,
`select_model`, `set_mode` and `tool_request` carry an optional `project_dir`.
They run on the connection's thread when it is in that project, else on any
running worker there, else on a fresh worker started in that project. Without
`project_dir` they run on the connection's thread. Their replies go to the
requesting connection only.

## Input history

The panel's arrow-up recall shares the CLI's shell-style input history: the CLI
appends panel-sent `user_message` frames to the same store the TUI's arrow-up
navigation uses (trimmed, consecutive duplicates skipped), and serves the
combined history back on request.

Client → CLI, list recent input history:

```json
{"type": "list_history", "project_dir": "/abs/path/to/project"}
```

CLI → client, the most recent entries, oldest first (empty when history
storage is unavailable; capped at 1000 entries):

```json
{"type": "history", "history": ["fix the tests", "task check"]}
```

Multi-line entries round-trip with real newlines in the JSON strings.

## Skills

The panel offers a "/" autocomplete of the agent's skills. It asks the CLI for
the merged, scope-tagged list the CLI already resolves (project, `.agents`,
user, plugin, catalog), so the menu mirrors what the TUI offers.

Client → CLI, list the available skills:

```json
{"type": "list_skills", "project_dir": "/abs/path/to/project"}
```

CLI → client, the discovered skills (empty when skills are unavailable):

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

Client → CLI:

```json
{"type": "list_models", "project_dir": "/abs/path/to/project"}
```

CLI → client, the configured model ids (empty when unavailable):

```json
{"type": "models", "models": ["anthropic/claude-sonnet-4-5", "ollama_cloud/deepseek-v4"], "current": "anthropic/claude-sonnet-4-5"}
```

- Each entry is a `provider/model` id exactly as the CLI would accept it.
- The first entry is the CLI's default model.

Client → CLI, switch the thread's model for its next turns (same effect as
`/model` in the TUI). The CLI answers with a fresh `models` frame, where
`current` shows whether the switch took effect, followed by a `mode` frame:

```json
{"type": "select_model", "model": "openai/gpt-4o"}
```

## Agent mode

The panel can toggle the thread's agent mode (the same state as the TUI's
shift+tab cycle; it also governs `tool_request` approvals). Modes travel as
their canonical keys: `standard`, `plan`, `auto`, `auto-with-judge`.

CLI → client, after every `set_mode` and `select_model`:

```json
{"type": "mode", "mode": "standard"}
```

Client → CLI, switch the mode. Unknown values are ignored; the CLI answers
with a fresh `mode` frame either way:

```json
{"type": "set_mode", "mode": "auto"}
```

## Artifacts (generated images)

Chat text can reference files the agent saved under the artifacts dir
(`~/.infer/projects/<project-slug>/artifacts/<...>`, e.g. `ImageGeneration`
output). An MV3 extension
cannot load a local file path in `<img>`, so alongside `/ws` the CLI serves that
directory read-only over HTTP. Only `infer chat`'s binding serves it, because
the artifacts dir is per project:

```text
GET http://127.0.0.1:<port>/artifacts/<relative-path>
```

The extension rewrites a markdown image whose URL contains `/artifacts/`
to this route (stripping the prefix through and including `artifacts/`) and
renders it inline. The route is loopback-only and unauthenticated (the artifacts
are the user's own generated files); path traversal is blocked by `http.Dir`.

## Tool approvals

Every approval uses one contract keyed by the tool call id, for agent tool calls
and panel `tool_request`s alike.

CLI → client, a CUSTOM AG-UI event, one per pending tool call. Every client of
the thread receives it:

```json
{
  "type": "CUSTOM",
  "name": "approval_request",
  "value": {"type": "approval_request", "tool_name": "Bash", "tool_args": "{\"command\":\"rm -rf ...\"}", "tool_call_id": "call_1"}
}
```

- `tool_args` is the raw tool-call arguments JSON string (may be empty).

Client → CLI, the user's decision:

```json
{"type": "approval_response", "tool_call_id": "call_1", "approved": true, "scope": "always"}
```

- `approved: false` rejects. `scope: "always"` also auto-accepts later calls of
  the same kind, and an absent `scope` approves this call only.
- The first answer wins. The CLI sends the other clients of the thread a CUSTOM
  event so they can clear their prompt:

```json
{"type": "CUSTOM", "name": "approval_resolved", "value": {"tool_call_id": "call_1"}}
```

- Later answers for the same `tool_call_id` are ignored, and so is an
  `approval_resolved` the client does not recognize.

## Tool calls (client → CLI)

A client can invoke any of the CLI's tools (see `docs/tools-reference.md`)
through the binding. The CLI routes each request through its **normal tool
execution pipeline** — the same permissions, allowlists, and approval flow as a
tool call made by the agent. The protocol stays generic: new capabilities need
no new frame types.

Client → CLI:

```json
{"type": "tool_request", "project_dir": "/abs/path/to/project", "id": "<uuid>", "tool_name": "Bash", "tool_args": "{\"command\":\"gh api user\"}"}
```

CLI → client, exactly one result per request id:

```json
{"type": "tool_result", "id": "<uuid>", "success": true, "output": "...", "error": ""}
```

- `tool_name`/`tool_args` use the same vocabulary as `approval_request`:
  `tool_args` is the raw tool-arguments JSON string.
- The approval flow applies: a CUSTOM `approval_request` whose `tool_call_id`
  is the request `id` may precede the result, and only the requesting client
  receives it. A denial produces `{"success": false, "error": ...}` — never a
  dropped id.
- `success: false` / `error != ""` means failure. For `Bash`, `output` is the
  combined stdout/stderr; a non-zero exit sets `success: false` with `output`
  still populated.
- An unknown `tool_name` yields one failed result, not a closed socket.
- The client enforces its own timeout and MUST ignore a late `tool_result`
  with an unknown id. If the socket drops before the result, the CLI drops it —
  no queuing or replay.
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
