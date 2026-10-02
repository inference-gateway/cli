---
name: config
description: >
  Change infer settings from chat. Use when the user types /config <request>
  (e.g. /config set the gateway timeout to 300, /config switch the model to
  anthropic/claude-sonnet-5-5, /config what is my max_turns) or asks to check
  or change an infer setting in config.yaml: it maps the request to the
  dotted config key, shows the current value, writes the change with
  `infer config set` only after the user confirms, and hands /reload back to
  apply it. Built-in and CLI-specific. Never for tool, bash allow-list,
  sandbox, MCP, channel, plugin, hook or judge policy, or for secrets.
license: Apache-2.0
---

# Changing infer settings

The user types `/config <request>` so they don't have to remember key names
or YAML layout. Your job is to find the right key, show what changes, and
write it through the CLI once they agree. The CLI already knows every key's
type, the file layering and the write path. Editing YAML by hand would skip
all of that and trip the approval prompts that guard `~/.infer/`.

Run every shell command as its OWN Bash call - no `|`, `&&`, `;`, `$(...)` or
redirects like `2>/dev/null`. The Bash gate rejects those.

## 1. Resolve the key

Map the request to one or more dotted keys (`gateway.timeout`,
`agent.model`, `agent.max_turns`, `chat.theme`, ...) and read each one:

    infer config get gateway.timeout

- A `config key ... not found` error means the key does not exist. Say so
  plainly and stop. Never write a guessed key.
- When you are unsure which key the user means, list a section to see its
  keys (`infer config get agent`) and ask ONE AskUserQuestion round with the
  candidates. If it is still unclear after that, stop and name the keys you
  found.
- Never run a bare `infer config get`, or `infer config get gateway` /
  `infer config get telemetry`. Those print the gateway API key and telemetry
  headers into the conversation. Read single leaves in those sections.

A question that only asks for a value ("what is my max_turns?") ends here:
answer it and stop.

## 2. Confirm

Show the change as `key: old -> new` and where it will be written:
`~/.infer/config.yaml` by default, or the project `.infer/config.yaml` only
when the user asked for a project override. Then ask with AskUserQuestion,
options `Apply` and `Cancel`.

Nothing is written without an explicit `Apply`. If the user cancels, or the
tool reports no interactive user (`available: false`), change nothing and
give them the exact command to run themselves:

    infer config set gateway.timeout 300

## 3. Apply

    infer config set gateway.timeout 300

Add `--project` only for a project override. List keys take a
comma-separated value that replaces the whole list, map keys such as
`context_windows` cannot be set this way.

Read the key back with `infer config get <key>`. If it still shows the old
value, a project `.infer/config.yaml` or an `INFER_*` environment variable
overrides the userspace file. Tell the user which one instead of writing
again.

End with: "Saved. Type /reload to apply it to this session - it tells you if
the key needs a restart instead." Don't claim the running session already
uses the new value. Only /reload knows which keys it can apply live.

## Out of bounds

These change what the agent may do or carry credentials, so they stay with
the user. Refuse, name the file to edit by hand, and never edit it yourself:

- Tool policy, bash allow-lists, approval behaviour (`tools.*`):
  `~/.infer/tools.yaml`. `infer config set tools.*` is rejected anyway.
- Filesystem sandbox: `~/.infer/sandbox.yaml`.
- MCP servers, channels, plugins, hooks, the judge: their own
  `mcp.yaml`, `channels.yaml`, `plugins.yaml`, `hooks.yaml`, `judge.yaml`
  under `~/.infer/`.
- Secrets such as `gateway.api_key` or `telemetry.otlp.headers`: give the
  `infer config set` command for the user to run in their own terminal, so
  the value never passes through the conversation.

Prompts and other settings kept in their own files (`prompts.yaml`,
`computer_use.yaml`, `browser_use.yaml`, ...) are not reachable with
`infer config set` either. Point to the file.

## Approval

`infer config get` and `infer config set` are not auto-approved by default:
each call goes through the normal approval gate unless the operator
allow-listed it in `tools.bash.mode.<mode>.allow`. Expect prompts in chat.
