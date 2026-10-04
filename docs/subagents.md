# Markdown Subagents

[← Back to README](../README.md)

Subagents can be defined as Markdown files with YAML frontmatter, the same
format Claude Code (`.claude/agents/*.md`) and Gemini CLI
(`.gemini/agents/*.md`) use. An existing agent file from either tool can be
copied into `.infer/agents/` unchanged. The frontmatter sets the agent's name,
when to use it, its model and which tools it may use; the Markdown body is its
system prompt.

Markdown agents are reusable presets for the `Agent` tool's arguments: the
main agent delegates work to them by name instead of spelling out a system
prompt each time. They are separate from `.infer/agents.yaml`, the A2A agent
registry - see [A2A Agents](agents-configuration.md) for that. Both listing
surfaces show the two kinds grouped: `infer agents list` renders the A2A
agents first, then the Markdown presets, and the `/agents` view in the TUI
does the same in one view.

## Table of Contents

- [Format](#format)
- [Locations and Precedence](#locations-and-precedence)
- [Tools Allowlist](#tools-allowlist)
- [Model](#model)
- [Headless Subagents](#headless-subagents)
- [Interactive Subagents](#interactive-subagents)
- [Run Stats](#run-stats)
- [Compatibility Notes](#compatibility-notes)
- [Limitations](#limitations)

---

## Format

```markdown
---
name: code-reviewer
description: Reviews a diff for correctness bugs. Use after making code changes.
model: deepseek/deepseek-v4-pro
tools: Read, Grep, Tree
---

You are a senior reviewer. Read the changed files and report findings as a
numbered list ordered by severity.
```

- `name` (required): identifier the main agent passes as the `Agent` tool's `agent` argument. Lowercase letters, digits,
  `-` and `_`, at most 64 characters. It does not have to match the file name.
- `description` (required): shown to the main agent so it knows when to delegate.
- `model` (optional): `provider/model`, or `inherit` to use the parent's model. See [Model](#model).
- `tools` (optional): tools the subagent may use. See [Tools Allowlist](#tools-allowlist).
- `disallowedTools` (optional): tools removed from the resolved list. Same format as `tools`.

Any other frontmatter key (`color`, `temperature`, `max_turns`, `maxTurns`,
`mcpServers`, `permissionMode`, ...) is accepted and ignored, so agent files
written for other orchestrators load unchanged.

The file is loaded once per session. Files with broken frontmatter, a missing
or invalid `name`/`description`, or a `tools` list that resolves to no known
tool are skipped with a warning naming the file and the reason; an invalid
file never fails startup.

## Locations and Precedence

Agent definitions are looked up in this order - first match wins on a name
collision:

1. **Project**: `.infer/agents/<name>.md`
2. **User-global**: `~/.infer/agents/<name>.md`

A project can therefore override a personal agent with the same name, the
same way project skills override user skills. Commit `.infer/agents/` with
your project to share its agents; `~/.infer/agents/` stays personal.

## Tools Allowlist

`tools` accepts a YAML list (Gemini CLI style) or a comma-separated string
(Claude Code style). When omitted, the subagent gets the parent's tools.

- Unknown tool names (for example Claude's `Glob`) log a warning and are
  dropped.
- `disallowedTools` entries are removed from the resolved list. Silently
  ignoring a deny list would give the subagent more tools than its author
  intended, so it is honored.
- A restriction that resolves to no known tool skips the file with a warning;
  an empty allowlist never falls back to all tools.

The resolved allowlist is passed to the spawned subagent through the
`INFER_SUBAGENT_TOOLS` environment variable (works in both headless and
tmux-interactive mode) and is enforced there in one place: a disallowed tool
is neither offered to the model nor executable by naming it. The `Agent` tool
itself can be listed to let a named agent spawn subagents - the usual
`tools.agent.max_depth` recursion guard still applies.

The subagent's capability (`type`) is derived from the resolved allowlist:
ReadOnly when every allowed tool is read-only, otherwise ReadWrite (mutations
still go through approval). With no allowlist the subagent keeps the parent's
tools and runs as ReadWrite.

While the parent run is in plan mode a subagent is always read-only, whatever
its `type` or its derived allowlist says: planning is a read-only mode, so a
delegated subagent may explore but never mutate the repo (see
[Plan Mode](plan-mode.md)).

## Model

`model` takes a `provider/model` reference (for example
`deepseek/deepseek-v4-pro`) or `inherit`, which uses the parent turn's model.

A value without a provider prefix - Claude aliases like `sonnet`, or bare
model IDs - logs a warning and falls back to `inherit`, so Claude Code files
still load. The file's model wins over a per-task `model` argument; when the
file sets none, the normal resolution order applies (per-task model,
`tools.agent.model`, then the parent turn's model).

## Headless Subagents

With `tools.agent.mode: headless` (the default) a delegated subagent runs as an
`infer headless --keep-alive` subprocess whose stdin the parent holds open. The
task is its first turn, and the subagent stays alive idle after it so the parent
can talk to it without respawning and losing its context:

- The parent receives one `[Subagent Completed: <label>]` note per finished turn,
  carrying that turn's final message and the run stats. A turn that ended in an
  error arrives as `[Subagent Failed: <label>]` with the error.
- `SendSubagentInput` with `text` sends the subagent a follow-up message - new
  information, a correction, a question about its result - which it runs as its
  next turn in the same session, and the parent is notified again when that turn
  ends. A message sent while the subagent is mid-turn reaches it right after
  its current tool call, so asking a running subagent to wrap up and report now
  works. Nothing is lost and no note is duplicated. `keys` and `submit=false` drive an
  interactive pane's TUI and fail for a headless subagent.
- A subagent that sits idle for `tools.agent.idle_timeout` seconds after a
  completed turn is closed with one `[Subagent Closed: <label>]` note carrying
  its last message. The default is `300` seconds, `0` disables the auto-close and
  `INFER_TOOLS_AGENT_IDLE_TIMEOUT` overrides it. There is no `[Subagent Idle]`
  warning for a headless subagent: every turn reports done, so the completion
  note is the cue that it awaits a follow-up.
- `CloseSubagent` stops an idle or running headless subagent at once.
- An idle subagent does not keep a headless parent alive: a one-shot
  `infer headless` run that delegated work exits once its own turn is done.
- `tools.agent.wait: true` (blocking fan-in) is unchanged: a blocking subagent
  returns its first turn and is not kept alive.

## Interactive Subagents

With `tools.agent.mode: interactive` a delegated subagent runs as a live
`infer chat` inside a tmux pane you can watch, opened as a vertical split.
Outside tmux the subagent falls back to headless. The pane is not a forever
REPL when the `Agent` tool drives it: the delegated task is one turn, and
when that turn completes, the subagent's chat writes the final assistant
message to its result file with `done: true`. A turn that ends with a
question is a completed turn too, so write self-contained task descriptions.

- The parent monitor delivers exactly one `[Subagent Completed: <label>]`
  note carrying that message, then closes the pane and records the subagent
  completed. A failed terminal turn (a result file with `success: false`)
  counts as done too and is closed the same way, with the error in the note.
- A pane that never reports done - its turn ended without any text, its
  TUI hung, or it runs an older binary without the done field - is closed
  after `tools.agent.idle_timeout` seconds of inactivity: no new
  result, no pane change and no pending approval. The close arrives as one
  `[Subagent Closed: <label>]` note carrying anything already harvested.
  The default is `300` seconds; `0` disables the auto-close and
  `INFER_TOOLS_AGENT_IDLE_TIMEOUT` overrides it.
- `[Subagent Idle: <label>]` warns early in the idle window - instead of the
  old `[Subagent Completed]` masquerade it names the pending auto-close, so
  the parent can re-prompt with `SendSubagentInput` (which resets the clock)
  or answer the prompt via `ApproveSubagent`.
- `CloseSubagent` is only for stopping a subagent early. In the normal case
  the parent never needs to close anything: one completion note per task,
  and no pile-up of idle panes.

## Run Stats

Every subagent reports what its run cost alongside its answer. The parent reads one line in the subagent's
result, in the blocking tool result and in the `[Subagent Completed: <label>]` note alike:

```text
Tools: 12 succeeded, 1 failed | Tokens: 60448 in, 745 out
```

- **Tools** counts the tool calls the subagent executed. A rejected call counts as failed.
- **Tokens** are the input and output tokens of the whole subagent session, every turn of a headless
  subagent included. `C.` is the slice of them the provider served from its prompt cache, in the same
  notation the status bar uses for the conversation itself, and is dropped while the run reported no cache hits.
  A run that reported no usage, such as an A2A agent that sends none, shows no token figures at all.

A failed tool call does not fail the subagent. A subagent fails only when its run ends with an error, so the
counts are how the parent tells a clean run from one that struggled. A subagent that crashes before it writes
its result file reports no stats.

The chat shows the same numbers. A subagent's row in the list under the composer carries a child line with the
tool counts and the total tokens. For a headless subagent the line is live: it counts up as the subagent works,
then settles on the reported totals for as long as the row lingers. An interactive subagent shows its line once
it finishes. An A2A task shows the usage its agent attaches to the task, which ADK agents do when the task ends:

```text
┌ npm run build shell       2s
│ reviewer      subagent ✓ 40s
│ └ 12 ✓ 1 ✗ · 61.2k tokens C.58.1k
└ tester        subagent ✗ 48s
  └ 3 ✓ 4 ✗ · 890 tokens
```

## Compatibility Notes

Claude Code agent files load as-is:

- comma-separated `tools` string: yes
- `disallowedTools`: honored
- `model: sonnet` (alias, no provider prefix): warning, falls back to inherit
- `color` and unknown keys: accepted and ignored

Gemini CLI agent files load as-is:

- `tools` as a YAML list: yes
- `temperature`, `max_turns`: accepted and ignored

## Limitations

- Reading `.claude/agents/` or `.gemini/agents/` directly is not supported
  yet - copy or symlink the files into `.infer/agents/`.
- No hot reload: files are scanned once per session.
- Per-agent `mcpServers`, `temperature`, `max_turns`, `permissionMode` and
  tool wildcards such as `mcp_*` are not honored in v1; MCP tools register
  after session start, so MCP tool names in a `tools` list are treated as
  unknown and dropped with a warning.
- Markdown agents are created by writing the file yourself - there is no
  scaffolding command yet (they are listed via `infer agents list` and the
  `/agents` view).

---

[← Back to README](../README.md)
