# Daemon Binding Protocol

Every client of [`infer daemon`](daemon.md) talks to the agent through one
localhost AG-UI WebSocket binding: the opentask
[extension](https://github.com/inference-gateway/opentask), the desktop app, and
`infer chat` or a standalone `infer headless` reaching the user's browser. Each
thread (a project dir plus a conversation id) runs in its own session worker,
and the binding relays the worker's AG-UI events to the thread's clients as bare
frames. The same socket lets the CLI drive the **user's real browser** through
the extension instead of a Playwright-launched one. This document is the wire
contract the clients implement. The events themselves are defined in
[AG-UI Output Format](ag-ui-output.md).

## Transport

- `infer daemon` listens on `ws://127.0.0.1:<port>/ws` (default port `52789`)
  when `daemon.yaml` sets `binding.enabled`, or when `browser_use` is enabled
  with `backend: extension`. The port and the token come from
  `daemon.yaml` → `binding.port` / `binding.token`, falling back to
  `browser_use.yaml` → `extension.port` / `extension.token`. Clients dial in —
  MV3 service workers cannot listen.
- Only the daemon binds the port. `infer chat` and a standalone `infer headless`
  reach the browser as browser clients through the daemon (`client: "browser"` in
  the hello) and start `infer daemon` when nothing is listening. A session
  worker's `browser_command` stdout lines are routed to the extension connection
  by the daemon, and each `browser_result` goes back to that worker by `id`.
  Commands serialize across threads because one browser serves them.
- The same listener serves plain HTTP routes beside `/ws`: `/artifacts/`, `/a2a`,
  the [web terminal](web-terminal.md) at `/terminal/` and a session's latest
  screen frame at `/screen/`. A terminal tab is not a binding client itself,
  the `infer chat --daemon` it runs in a PTY is.
- The daemon started this way runs in the background and outlives the `infer`
  process that started it. It shares that process's terminal, so closing the
  terminal stops it too, and the next browser call starts another. It loads the
  config of the directory it was started from and runs everything that config
  enables, including channels, the scheduler and the heartbeat. Stop it by
  signalling the pid in `~/.infer/run/daemon.pid`.
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

Enable with either file:

```yaml
# ~/.infer/daemon.yaml, the binding on its own
binding:
  enabled: true
  port: 52789
  token: <shared secret>
```

```yaml
# ~/.infer/browser_use.yaml, the binding together with browser use
enabled: true
backend: extension
extension:
  port: 52789
  token: <shared secret; infer init seeds one>
```

## Handshake

Client → CLI, first frame, within 5 seconds of connecting:

```json
{"type": "browser_hello", "token": "<shared secret>", "client": "extension", "extension_version": "1.9.2"}
```

- `client` names the kind of client: `extension`, `desktop`, `browser`, `tui` (`infer chat --daemon`),
  `cli` (`infer daemon` subcommands), `subagent-parent` or `daemon` (a peer daemon). Only `extension`, or
  an absent value, is the browser extension. Every other value is a client and never takes the
  extension's place.
- Any hello with a valid token is accepted.

CLI → client on success (on failure the socket is closed):

```json
{"type": "browser_hello_ack"}
```

The ack carries the host facts the daemon declares, such as `workspace`, `version` and `storage_id`
(see [Daemon to daemon](#daemon-to-daemon)). The frames are the AG-UI 1.0 ones: interrupts, run
`usage`, the one state object, `ACTIVITY_SNAPSHOT` and `run_agent_input`.

## Browser extension status

CLI → client, the state of the extension connection, sent to every client that
is not the extension: once when the client connects, and again whenever the
extension attaches or detaches. The frame for a client that just connected names
the state at that moment, so its first frame is always a status one:

```json
{"type": "browser_extension_status", "connected": true, "extension_version": "1.9.2", "protocol_version": 1}
```

- `connected` is whether an extension is attached right now.
  `extension_version` is the version the attached extension declared in its
  hello, and travels only while one is attached.
- `protocol_version` is this frame's own schema version (`1` today), independent
  of the handshake's, for clients to gate what they render or how they parse the
  frame.

There is deliberately no frame and no CUSTOM event for pausing or resuming
browser use: a client stops the run, which ends with outcome `cancelled`, and
continues with a new run on the thread.

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
connection per thread. The only frame the CLI sends on connect is the extension status frame, so a client must ask for a thread itself.

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
  paths added to the user's `sandbox.yaml` allowed list), `max_turns` and
  `subagent` (`{"parent": "<conversation id>", "depth": 1, "tools": [...]}`,
  set by a worker's Agent tool when the thread is a subagent it spawned).
- `agent` names a [Markdown agent](subagents.md) the thread runs as, looked
  up in `<project_dir>/.infer/agents/` and then `~/.infer/agents/`. The daemon
  the thread is opened on resolves it before the worker launches, the same
  overlay a named subagent gets: the agent's body is the system prompt, a
  front-matter `model` replaces `model` and stays fixed for the thread, and
  `tools` / `disallowedTools` restrict the worker's tools. An empty, `inherit`
  or alias model keeps the thread's `model`, and `custom_instructions` still
  append. The agent resolves into `model`, `system_prompt`, `tools`,
  `disallowed_tools` and `pin_model`, which a workspace relay and a teleport
  carry instead of the name. An unknown agent fails the open with a
  `RUN_ERROR` naming the available agents.
- `workspace` and `workspace_dir` place a thread whose conversation no lease
  places: the registered workspace that holds it and its source code there.
  A client sends them when it opens a session it listed on a remote
  workspace (`list_conversations` on a relayed thread). The daemon relays the
  thread there and never starts it locally, even when the workspace cannot
  be reached.
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

Client → CLI, start the thread's next run with a message (queued if the agent
is busy, exactly like typing in the TUI). `input` is an AG-UI `RunAgentInput`
whose `messages` holds the new messages only, since the worker owns the history:

```json
{
  "type": "run_agent_input",
  "input": {
    "threadId": "<conversation id>",
    "runId": "<uuid>",
    "messages": [{"id": "<uuid>", "role": "user", "content": [
      {"type": "text", "text": "please also check the docs page"},
      {"type": "image", "mimeType": "image/png", "data": "<base64>", "filename": "shot.png"}
    ]}]
  }
}
```

- `content` is a string or content parts. Each file part is saved to the
  project's tmp dir. PNG, JPEG, GIF and WebP images reach the model as image
  parts, and other files as a note naming the saved path. Files over 10 MiB are
  skipped.
- One frame is one control line, capped at 160 MiB to accommodate ten
  10 MiB attachments after base64 encoding. Oversized lines are discarded
  without closing the control channel.
- An input with no messages and no `resume` continues the thread after a
  stopped run, which is how a paused computer-use session resumes.
- A `#N` token in a text part, at the start or after whitespace, is expanded
  by the worker before the run: the issue's title, body and most recent
  comments replace the token, the way the chat TUI expands it. A failed
  fetch leaves the raw token in place.
- An `@<path>` token in a text part is expanded by the worker before the
  run, the way the chat TUI expands it: a text file is inlined as a fenced
  block, a Markdown file as its `## Summary` section when it has one, and an
  image becomes a note naming its path. A path that does not resolve to a
  readable file within the size limits stays in place as the raw token.

Client → CLI, stop the turn currently streaming (a no-op when nothing runs):

```json
{"type": "interrupt"}
```

When the CLI cannot route a frame (a relative `project_dir`, a `run_agent_input`
before any `new_session`, a worker that fails to start), it answers the sender
with a `RUN_ERROR` that has no `runId`. When a thread's worker exits mid-turn,
every client of the thread gets a `RUN_ERROR` carrying the open run's `runId`.
The next `run_agent_input` starts a new worker for the thread.

Client → CLI, stop a thread's worker at once, which is what a client sends
when it deletes the conversation instead of leaving the worker to idle out:

```json
{"type": "stop_thread", "project_dir": "/abs/path/to/project", "id": "<conversation id>"}
```

- Followers of the thread with a run open get a `RUN_ERROR` for it, exactly
  as when a worker exits mid-turn, and a thread with no running worker is
  left alone.
- When another workspace owns the session, the CLI deletes the session's
  runtime there (a Kubernetes `Orchestrator` with its volumes) and releases the
  lease, so the session continues from the CLI's own copy.
- When the session teleported home, the CLI deletes the runtime it left on the
  workspace it came from.

CLI → client, only the sender receives it:

```json
{"type": "thread_stopped", "project_dir": "/abs/path/to/project", "id": "<conversation id>", "workspace": "k8s-dev"}
```

- `workspace` names the workspace whose runtime was deleted, and is
  absent when the session ran on the CLI itself. `error` replaces it when the
  runtime could not be deleted, and the lease then stays.

## Active threads

CLI → client, to every connected client whenever a thread opens or ends a run,
so a client that follows no thread can still see what streams elsewhere - the
desktop app runs its threads on its own connections:

```json
{"type": "thread_activity", "threads": [{"project_dir": "/abs/path/to/project", "conversation_id": "<uuid>"}]}
```

- `threads` names every thread with an open run, sorted, empty when none. The
  frame carries no AG-UI events and is advisory: a client that follows a thread
  ignores it, and one that follows nothing may offer a listed thread to the
  user and follow it with `resume_conversation` to mirror its stream.
- Clients that know nothing of this frame ignore it, per the unknown-type rule.

## Interrupts

A tool call that needs approval or an `AskUserQuestion` form suspends the run:
it ends with `RUN_FINISHED` whose `outcome` is `interrupt`. Every client of the
thread receives it:

```json
{"type": "RUN_FINISHED", "threadId": "<conversation id>", "runId": "<run>", "outcome": {"type": "interrupt", "interrupts": [
  {"id": "call_1", "reason": "tool_call", "toolCallId": "call_1"}
]}}
```

- `reason` is `tool_call` for an approval, with the `toolCallId` of the call
  and `metadata` naming it (`toolName`, `arguments`), plus a `message` with the
  approval's context when there is one. A `SandboxAccess` grant is never
  streamed as a tool call, so the interrupt is all a client sees of it.
- `reason` is `input_required` for a question, with a `responseSchema`
  describing the answer's `payload` and `metadata.questions` listing the
  questions.
- `reason` is `plan_approval` for a plan the agent asks to execute, with the
  plan in `metadata` (`planId`, `plan`). The answer's `payload` is
  `{"action": "accept" | "accept_standard" | "reject"}`, and an accepted plan
  runs as the next turn in a new conversation, whose id the `RUN_STARTED`
  that follows carries as `threadId`.

Client → CLI, the user's decision, as a `run_agent_input` carrying resume
entries and no messages:

```json
{"type": "run_agent_input", "input": {"resume": [{"interruptId": "call_1", "status": "resolved"}]}}
```

- `resolved` approves the call, `cancelled` declines it or dismisses the form.
  A question's answer travels in the entry's `payload`.
- A resume answers every open interrupt of the run. The daemon refuses one that
  misses an interrupt with a `RUN_ERROR` naming the missing ids.
- A client that starts following the thread while its run is suspended gets
  the suspended `RUN_FINISHED` again right after its `MESSAGES_SNAPSHOT`. A
  worker whose run waits on an open interrupt is kept for at least the
  5 minute approval timeout after its last client left, so a reattaching
  client can still answer.
- The first resume wins. The daemon drops the later ones for the same
  interrupts, and the thread's other clients learn the decision from the
  `RUN_STARTED` of the continuation run that follows, whose `STATE_SNAPSHOT`
  carries the interrupted run's state. That run writes the `TOOL_CALL_RESULT`
  for the same `toolCallId`.

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
- `token_stats` (`request_count`, `total_input_tokens`, `total_output_tokens`, …)
  and `cost_stats` (`TotalCost`, `Currency`, …) carry the session's usage, the
  columns of the chat TUI's conversation picker.
- The list is capped at 50 conversations, the 50 most recently updated.
- The array is empty when the project runs without conversation persistence
  (`storage.enabled: false`).

## Session artifacts

Client → CLI, list the files a session saved under its artifacts dir
(`~/.infer/projects/<project-slug>/artifacts/<conversation id>/`), which is
what the chat TUI's `/artifacts` view and its `⬇ artifacts` indicator ask for:

```json
{"type": "list_artifacts", "project_dir": "/abs/path/to/project", "id": "<conversation id>"}
```

CLI → client, newest-first (sorted by `modified_at` descending):

```json
{"type": "artifacts", "id": "<conversation id>", "artifacts": [{"name": "20261028-202514.mp4", "size": 60416, "modified_at": "2026-10-28T20:25:18Z"}]}
```

Client → CLI, read one of them:

```json
{"type": "read_artifact", "project_dir": "/abs/path/to/project", "id": "<conversation id>", "name": "20261028-202514.mp4"}
```

CLI → client:

```json
{"type": "artifact", "id": "<conversation id>", "name": "20261028-202514.mp4", "data": "<base64>"}
```

- Both run on the worker of the workspace that owns the session, so a session
  that teleported lists and reads the artifacts in its pod. The daemon relays
  them there like any other project-scoped request, resuming a suspended
  Orchestrator on the way, and an unreachable owner answers with a `RUN_ERROR`
  without a `runId`.
- Only the files directly under the session's artifacts dir are listed or read.
  `name` is one file name, so a path, `..` or an empty name is refused and a
  session that saved nothing yet lists as empty.
- `data` carries the whole file in one frame, capped at 64 MiB, the binding's
  frame limit. A larger file answers `error` naming its size.
- `error` replaces the payload when the dir cannot be read or the file is
  missing.

## Session screen

Client → CLI, the latest frame of the screen a session's worker streams
(`computer_use.screenshot.streaming_enabled`), which the web terminal's live
view polls through `GET /screen/?id=<conversation id>` on the same listener:

```json
{"type": "read_frame", "project_dir": "/abs/path/to/project", "id": "<conversation id>"}
```

CLI → client:

```json
{"type": "frame", "id": "<conversation id>",
 "frame": {"id": "f-1", "timestamp": "2026-11-04T12:00:00Z", "data": "<base64>", "width": 1280, "height": 800, "format": "jpeg"}}
```

- The frame comes from the worker that runs the session, so it is the screen
  of the workspace the session teleported to, relayed by the daemon like
  `read_artifact`.
- With `project_dir` it runs only on the thread of the session `id` names,
  relaunching that session's worker when it is not running. A session that is
  not open there answers a `RUN_ERROR`, never another session's screen.
- `error` replaces `frame` when the worker streams no screen or captured
  nothing yet.

The same listener takes the screen's take-over, for the web terminal's
**Take over**: `POST /screen/?id=<conversation id>` with the same bearer token
(the body also accepts `{"type": "screen_control", "project_dir": "...", "id": "...",
"action": "take_over" | "hand_back"}` and `{"type": "screen_input", "project_dir":
"...", "id": "...", "input": {...}}` in the `frame`-response shape of the panel):

```json
{"type": "screen_control", "project_dir": "/abs/path/to/project", "id": "<conversation id>", "action": "take_over"}
```

CLI → client, one reply per request id:

```json
{"type": "screen_result", "id": "<request id>", "held": true}
```

- `take_over` claims the screen for the person - the second claim while it is
  held, from another tab or viewer, errors - and resolves as rejected the
  approval box the worker's `Computer` call parks on, so the person decides.
- `hand_back` releases the screen and queues the worker's continue message.
- `screen_input` carries one person input in the frame space the GET's image
  answers in: `{"kind": "move" | "click" | "double_click" | "scroll" | "type" |
  "key", "x", "y", "text", "combo", "button", "direction", "amount"}`. It drives
  the display directly - no approval, no judge and no conversation entry.
- An input for a screen nobody holds errors, and the HTTP POST answers it with
  `409 Conflict`, so a stale tab cannot drive what it handed back.

## Project-scoped requests

`list_conversations`, `list_history`, `list_skills`, `list_shortcuts`,
`list_issues`, `list_files`, `list_tasks`, `cancel_task`, `list_models`,
`select_model`, `set_mode`, `tool_request`, `completion_request`,
`list_artifacts`, `read_artifact` and `read_frame` carry an optional `project_dir`.
They run on the connection's thread when it is in that project, else on the
live thread of the session `id` names, else on any running worker there, else
on a fresh worker started in that project, except `read_frame` (see
[Session screen](#session-screen)). Without `project_dir` they run on the
connection's thread. Their replies go to the requesting connection only.

## Notes

Client → CLI, keep text the client produced in the conversation of the connection's thread, such as the output of a
command it ran itself:

```json
{"type": "add_note", "content": "..."}
```

The frame has no reply and starts no run. The worker stores the content as an assistant entry, so the model sees it
on the next turn and the conversation keeps it. A thread with no running worker drops the note.

## Input history

The panel's arrow-up recall shares the CLI's shell-style input history: the CLI
appends the text of panel-sent `run_agent_input` messages to the same store the
TUI's arrow-up navigation uses (trimmed, consecutive duplicates skipped), and
serves the combined history back on request.

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

## Shortcuts

The chat TUI's "/" menu also lists the shortcut registry: the built-in
commands and the custom shortcuts defined in YAML. A client asks the CLI for
those entries and merges them with the skills of `list_skills` itself. Where
a skill and a registry entry share a name the TUI keeps the registry entry.

Client → CLI, list the shortcut registry:

```json
{"type": "list_shortcuts", "project_dir": "/abs/path/to/project"}
```

CLI → client, the registry's entries sorted by name:

```json
{"type": "shortcuts", "shortcuts": [{"name": "clear", "description": "Clear the conversation", "usage": "/clear", "kind": "command"}]}
```

- `name` is the entry's name without the leading "/".
- `kind` is `command` for a built-in and `shortcut` for a custom shortcut, the
  badge the TUI shows.
- The frame only lists. The worker does not run a `/<name>` it receives in a
  `run_agent_input` message, so a client runs what it offers: a snippet
  shortcut through `completion_request`, and anything else through its own
  command surface.

## GitHub issues

The panel offers a "#" autocomplete of the current project's GitHub issues,
the menu the chat TUI's input offers. It asks the CLI for the open issues the
GitHub issue service resolves for the worker's project dir. A `#N` token a
client sends in a `run_agent_input` message is expanded by the worker the
same way, so a daemon client types "#" exactly like the TUI.

Client → CLI, list the project's open issues:

```json
{"type": "list_issues", "project_dir": "/abs/path/to/project"}
```

CLI → client, the open issues, newest first (empty wherever the TUI's trigger
shows nothing - no git repo, no `gh`, no auth, or a failed `gh` call):

```json
{"type": "issues", "issues": [{"number": 57, "title": "Fix the flaky test", "state": "OPEN"}]}
```

- `number` and `title` are the issue's own, `state` is its GitHub state as
  the issue service's list returns it.
- The list is the service's recent-open-issues list, so its internal cap
  applies.

## Project files

The panel offers an "@" autocomplete of the current project's files, the menu
the chat TUI's input offers. An `@<path>` token a client sends in a
`run_agent_input` message is expanded by the worker the same way.

Client → CLI, list the project's files:

```json
{"type": "list_files", "project_dir": "/abs/path/to/project"}
```

CLI → client, the files the worker offers for `@<path>` expansion (empty when
the project dir cannot be walked):

```json
{"type": "files", "files": ["cmd/infer/main.go", "README.md", "~/.infer/config.yaml"]}
```

- Paths are relative to the worker's project dir, followed by the user's
  `~/.infer` files as `~/.infer/<path>`. Each is usable as-is after an "@".
- Hidden files, ignored directories and files over the expansion size limits
  are left out, as in the TUI's menu.

## Background jobs

A worker's shells, subagents, recordings and A2A tasks live in that worker, so a
client lists and stops them there. Follow the session first, since these frames
answer for the worker they reach.

Client → CLI, list the jobs:

```json
{"type": "list_tasks", "project_dir": "/abs/path/to/project", "id": "<conversation id>"}
```

CLI → client, the running and recently finished jobs:

```json
{"type": "tasks", "tasks": [{"id": "shell-1", "kind": "shell", "label": "shell-1", "detail": "go build ./...", "status": "running", "started_at": "2026-11-06T10:00:00Z"}]}
```

- `status` is `running`, `completed` or `failed`.
- A finished job also has `completed_at` and `output`. A subagent has its
  `session_id`, and `stats` once it ends.

Client → CLI, stop a job:

```json
{"type": "cancel_task", "project_dir": "/abs/path/to/project", "id": "<conversation id>", "task_id": "shell-1"}
```

CLI → client, once the job was told to stop. `error` says why it could not be:

```json
{"type": "task_cancelled", "task_id": "shell-1"}
```

The job ends a moment later, so list the jobs again to see it finished.

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
- `catalog`, when the worker fetched the gateway's models, holds their gateway
  entries (`id`, `context_window`, `pricing`, `modalities`), the details the
  chat TUI's model picker shows.

Client → CLI, switch the thread's model for its next turns (same effect as
`/model` in the TUI). The CLI answers with a fresh `models` frame, where
`current` shows whether the switch took effect, followed by a `mode` frame.
A thread whose `agent` pins a model keeps it, and `current` still reports it:

```json
{"type": "select_model", "model": "openai/gpt-4o"}
```

## Completions

A client can ask the thread's model one question outside any run, the way the
chat TUI's snippet shortcuts (`/git commit`) do in a daemon chat. The worker
answers with its own gateway and model, so the call runs wherever the session
runs, a remote workspace included.

Client → CLI:

```json
{"type": "completion_request", "project_dir": "/abs/path/to/project", "id": "<uuid>", "model": "openai/gpt-4o", "prompt": "...", "max_tokens": 1000}
```

CLI → client, exactly one result per request id:

```json
{"type": "completion_result", "id": "<uuid>", "content": "...", "reasoning": "", "error": ""}
```

- `model` is optional. Without it the call runs on the thread's current model.
  `system` and `max_tokens` are optional too.
- `content` is the answer. A reasoning model that put its whole answer in its
  reasoning leaves `content` empty and the answer in `reasoning`.
- `error != ""` means the call failed. The result is matched by `id`, so
  concurrent requests each get their own.

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
output). An MV3 extension cannot load a local file path in `<img>`, so it
rewrites a markdown image whose URL contains
`/.infer/projects/<project-slug>/artifacts/<relative-path>` onto this route and
renders it inline:

```text
GET http://127.0.0.1:<port>/artifacts/<project-slug>/<relative-path>
```

The daemon serves the file read-only from that project's artifacts dir. The
route is unauthenticated, because a browser `<img>` tag cannot send the token,
but it rides the binding's loopback listener and answers only from inside
`~/.infer/projects/<project-slug>/artifacts/`: a slug that is not a single
local path segment, or a relative path that does not stay under the artifacts
dir (`filepath.IsLocal`), answers 404, as do missing files and directories.
Listing a session's artifacts, or reading one from the workspace that owns the
session, goes over the authenticated socket instead, see
[Session artifacts](#session-artifacts).

## Tool approvals

An agent tool call that needs approval suspends the run as an interrupt (see
[Interrupts](#interrupts)). A panel `tool_request` runs outside any run, so its
approval is the one CUSTOM event left, the extension point AG-UI keeps for what
the protocol has no event for.

CLI → client, only the requesting client receives it:

```json
{
  "type": "CUSTOM",
  "name": "approval_request",
  "value": {"type": "approval_request", "tool_name": "Bash", "tool_args": "{\"command\":\"rm -rf ...\"}", "tool_call_id": "<request id>"}
}
```

- `tool_args` is the raw tool-call arguments JSON string (may be empty).

Client → CLI, the user's decision:

```json
{"type": "approval_response", "tool_call_id": "<request id>", "approved": true}
```

- `approved: false` rejects. The panel reads only `tool_call_id` and `approved`,
  so a `scope` field is ignored: an approval decides the call it names and
  nothing else.
- The first answer wins. Later answers for the same `tool_call_id` are
  ignored. The other clients of the thread that saw the request receive
  `{"type": "CUSTOM", "name": "approval_resolved", "value": {"tool_call_id": "<request id>"}}`.

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
  is the request `id` may precede the result (see [Tool approvals](#tool-approvals)).
  A denial produces `{"success": false, "error": ...}` — never a dropped id.
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

## Workspaces

A workspace is where a session's agent runs: this daemon, a standalone daemon reachable over TLS, or a
Kubernetes `Orchestrator` per session. `~/.infer/daemon.yaml` registers them by name, see
[infer daemon](daemon.md#workspaces-and-teleports).

Client → CLI:

```json
{"type": "list_workspaces"}
```

CLI → client, only the requesting client receives it:

```json
{
  "type": "workspaces",
  "workspaces": [
    {"name": "laptop", "kind": "local", "reachable": true, "capabilities": {"version": "0.238.0", "storage": "jsonl"}},
    {"name": "k8s-dev", "kind": "kubernetes", "reachable": false, "error": "the Inference Gateway operator is not installed ..."}
  ]
}
```

- The local workspace comes first. An unreachable workspace is reported with its `error`, never hidden.
- `capabilities` is what the workspace declared about itself: version, storage id, orchestrator count, image, auth.

## Teleports

A teleport sends a session to another workspace. The source daemon quiesces the thread's worker, packs a
checkpoint and streams it to the target's daemon, the target restores it and claims the session's lease,
and the source tells the thread's clients where the session lives now. The checkpoint carries the
conversation unless both daemons share one storage backend, the interrupts the last run was suspended
on, a git bundle of `HEAD` with the credential-free `origin` URL and a patch of the uncommitted and
untracked changes (minus what `sandbox.yaml` denies), the listed extra files, and the workspace's
`forward_env` values sealed to the target's certificate. A teleport happens between turns: an open run
is interrupted first.

Client → CLI:

```json
{
  "type": "teleport_session",
  "teleport_id": "<uuid>",
  "project_dir": "/abs/path/to/project",
  "id": "<conversation id>",
  "target": "k8s-dev",
  "files": ["build/out.bin"]
}
```

CLI → client, only the requesting client receives it, once per stage:

```json
{"type": "teleport_progress", "teleport_id": "<uuid>", "stage": "provisioning"}
```

- `stage` is one of `provisioning`, `checkpointing`, `restoring`, `ready` and `failed`. A `failed` stage
  carries `message` and leaves the session on the source with its lease intact.
- `teleport_id` is the client's idempotency key: a retry with the same id after `ready` answers `ready`
  at once and teleports nothing twice.
- A session another live workspace owns is refused with `failed`, unless `target` names the daemon it
  left, which brings it back (see [Daemon to daemon](#daemon-to-daemon)).
- `files` are extra files relative to the project that travel although git does not track them. A file
  outside the project, or one `sandbox.yaml` denies, fails the teleport.

```json
{"type": "teleport_cancel", "teleport_id": "<uuid>"}
```

Cancels a teleport before `ready`. The session stays on the source, which relaunches its worker on the
next frame.

CLI → the requesting client and every client of the thread, after `ready`:

```json
{
  "type": "session_teleported",
  "session": {"project_dir": "/abs/path/to/project", "conversation_id": "<conversation id>", "slug": "project"},
  "workspace": "k8s-dev",
  "addr": "",
  "workspace_dir": "/workspace/project/<conversation id>",
  "cancelled_interrupts": [{"id": "<interrupt id>", "reason": "tool_call", "toolCallId": "<tool call id>"}]
}
```

- Clients keep using `resume_conversation` with their own `project_dir`: the source daemon relays the
  thread to the owning workspace's daemon and rewrites `project_dir` to `workspace_dir` on the way. Killing
  the source daemon after `ready` does not touch the target.
- `addr` is the target's binding when a client can dial it directly (a daemon-kind workspace), with
  `project_dir` set to `workspace_dir`. It is empty for a Kubernetes workspace reached through a port-forward.
- `cancelled_interrupts` are the interrupts the suspended run was waiting on, as its `RUN_FINISHED`
  listed them. The target's worker closes the matching tool calls as cancelled, so the client tells the
  user and sends the next `run_agent_input` without a `resume`.

### Daemon to daemon

A daemon dials another daemon's binding with `client: "daemon"` and a `workspace` hello field naming
itself. The ack carries `workspace`, `version` and `storage_id`, so the dialer checks it reached the
workspace it meant and whether the two share a storage backend. The checkpoint streams as
`teleport_chunk` frames of at most 4 MiB and is closed by `teleport_import`:

```json
{"type": "teleport_chunk", "teleport_id": "<uuid>", "seq": 0, "data": "<base64>"}
{"type": "teleport_import", "teleport_id": "<uuid>", "workspace": "k8s-dev", "chunks": 3, "sha256": "<hex>"}
{"type": "teleport_imported", "teleport_id": "<uuid>", "ok": true, "workspace_dir": "/workspace/project/<conversation id>"}
```

The target validates the checkpoint (schema version, digest, the patch applies cleanly) before it
restores the repository, imports the conversation through a worker there and claims the lease. The source
then sends `resume_conversation` on the same connection and reports `ready` only when the
`MESSAGES_SNAPSHOT` renders to the checkpoint's digest. `teleport_abort` with the `teleport_id` undoes an
import whose verification failed. A daemon accepts these frames only from connections of kind `daemon`
and only when `workspace` names itself.

A session comes back the other way over a connection the daemon it left opens to its owner. That daemon
sends `teleport_return` with the workspace dir its lease names, the owner answers `teleport_returning` at once,
quiesces the worker, then streams the checkpoint back as `teleport_chunk` frames closed by `teleport_import`
naming the requester. The checkpoint always carries the conversation. Once the repository landed, the lease
teleported and the conversation imported, the requester sends `teleport_returned` and the owner releases its row.
An owner packs only a workspace dir under its own projects root and refuses a session another workspace owns.

```json
{
  "type": "teleport_return", "teleport_id": "<uuid>", "id": "<conversation id>",
  "workspace_dir": "/workspace/project/<conversation id>", "workspace": "laptop"
}
{"type": "teleport_returning", "teleport_id": "<uuid>", "id": "<conversation id>"}
{"type": "teleport_returned", "teleport_id": "<uuid>"}
```

### A2A peer endpoint

A workspace whose entry names `protocol: a2a` carries this same daemon-to-daemon frame vocabulary over
A2A 1.0 JSON-RPC instead of the WebSocket. Its binding serves `POST /a2a` on the same listener, behind the
same auth (the bearer token or a client certificate the listener verified), and the daemon at the far
end speaks the frames of [Daemon to daemon](#daemon-to-daemon) unchanged.

- `GET /a2a` is the agent card of the peer endpoint, an A2A 1.0 card naming its version and interface.
- `SendStreamingMessage` binds one exchange: the reply is a server-sent events stream whose first event
  announces the host (workspace, version, storage id, the ack's facts) and whose further events carry
  one frame each as a text part of an A2A message. The stream stays open for the exchange's life.
- `SendMessage` puts one frame on the exchange the message's `contextId` names, and answers with a
  completed task. Frames the frame handling produces ride the stream, never the reply.

A dial into an endpoint that serves nothing over a2a fails before the exchange can carry anything, so a
workspace that cannot serve its chosen protocol fails a teleport at the provisioning stage, before it
creates anything.

### Conversation export and import

Workers answer two panel frames the daemon uses to teleport a conversation between storage backends, since the
worker runs in the project dir whose storage layout the conversation lives in:

```json
{"type": "export_conversation", "project_dir": "/abs/path/to/project", "id": "<conversation id>"}
{"type": "conversation_export", "id": "<conversation id>", "entries": [...], "metadata": {...}, "message_count": 4, "digest": "<hex>"}
{"type": "import_conversation", "project_dir": "/workspace/project/<conversation id>", "id": "<conversation id>", "entries": [...], "metadata": {...}}
{"type": "conversation_imported", "id": "<conversation id>", "message_count": 4, "digest": "<hex>"}
```

- `digest` fingerprints the `MESSAGES_SNAPSHOT` the entries render to, without message ids, so two daemons
  agree a restored conversation matches.
- A worker without conversation storage answers with `error` set.

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
