# Plan Mode

[← Back to README](../README.md)

Plan Mode is a read-only operating mode for the agent. The model can use
`Read`, `Grep`, `Tree`, and `TodoWrite` to investigate the codebase, but it
cannot write, edit, delete, or run shell commands. Instead, it produces a
**plan** for the proposed change, persists that plan through the configured
storage backend (on jsonl as a Markdown file under `~/.infer/plans/`), and
surfaces it to you for approval before any real work happens.

## Why use it

Plan mode exists for tasks where you want a human gate between
"understand the change" and "make the change":

- Risky or far-reaching refactors.
- Migrations and schema changes.
- Multi-file features where you want a written record before execution.
- Pair-programming flows: review the plan, push back, iterate.

Every plan - accepted **or** rejected - is kept in storage (on the jsonl
backend under `~/.infer/plans/`) as an audit trail. You can grep, share, or
revisit them later.

## How to enter plan mode

In the interactive chat TUI, press **Shift+Tab** to cycle the agent mode.
The status line shows the current mode; cycle until it reads
**Plan Mode**. Press Shift+Tab again to leave.

The cycle is:

```text
Standard → Plan Mode → Auto-Accept → Standard → …
```

When a plan is **accepted**, the mode automatically switches to Auto-Accept
(or to Standard if you chose "Approve Each Step") and the agent begins
executing.

## What the model is told to do

The default plan-mode system prompt (defined in
`config/prompts.go::DefaultPromptsConfig`, key `agent.system_prompt_plan`)
instructs the model to:

1. Use `Read`, `Grep`, `Tree` to investigate.
2. Ask clarifying questions in regular assistant turns first - **not**
   call `RequestPlanApproval` while gaps remain.
3. Only call `RequestPlanApproval` when the plan is complete.
4. Pass two arguments to that tool:
   - `title`: a short phrase (≤ 60 chars, no slashes or `..`).
   - `plan`: the plan body as Markdown using the standard sections below.

## Plan structure

The plan body is a Markdown document using these H2 sections, in this
order. Sections that don't apply to the task are omitted; no extra
top-level sections are introduced.

- `## Context` - why this change is being made: problem, trigger,
  intended outcome.
- `## Files to Modify` - bullet list of paths plus a one-line note per
  file.
- `## Current Code` - short snippets of the existing code being changed
  (skip for new files).
- `## Changes` - concrete edits: function names, signatures, what's
  added / removed / replaced.
- `## Performance Impact` - expected runtime / memory / I/O impact.
  "Negligible." is a valid answer.
- `## Critical Files` - files other code depends on and that must remain
  backward-compatible.
- `## Edge Cases` - inputs and conditions needing explicit handling,
  plus the handling.
- `## Verification` - concrete steps you can run to confirm the change
  works end-to-end.

The H1 of the saved file is derived from the `title` argument, e.g.:

```markdown
# Add Login Flow

## Context
…
```

## Where plans are saved

Plans are persisted through the configured storage backend (`storage.type`:
jsonl, sqlite, postgres, redis, or d1). Every plan gets a stable ID of the
form `<YYYY-MM-DD-HHMMSS>-<slug>` (UTC timestamp), returned by the tool as an
`infer://plans/<id>` URI. Retrieve a plan from any backend with:

```bash
infer plans show <id>       # full markdown body
infer plans list            # all plans with title + creation time
```

On the default `jsonl` backend the plan also lands as a plain markdown file:

```text
~/.infer/plans/<YYYY-MM-DD-HHMMSS>-<slug>.md
```

Plans are user-global, not per-project - every project's plans land in the
same userspace directory. Writes are atomic (`<path>.tmp` + rename), so
you'll never see a half-written plan file.

`<slug>` is the title lowercased, with non-alphanumerics collapsed to
`-`, capped at 60 characters. A plan with the same title in the same second
upserts the previous record instead of creating a duplicate.

Example listing:

```bash
$ ls ~/.infer/plans/
2026-04-25-091230-rip-out-old-auth-middleware.md
2026-04-26-143015-add-login-flow.md
2026-04-27-103015-rename-config-loader.md
```

## Accept / Reject UX

When the model calls `RequestPlanApproval`, the chat TUI:

1. Renders the saved plan in a dedicated panel.
2. Shows a status line: `Plan ready - use arrow keys to select and Enter
   to confirm`.
3. Offers three options:

   - **Accept** (`Enter`/`y`) - agent switches to Auto-Accept mode (no
     per-tool approval prompts) and executes the plan.
   - **Reject** (`n`) - chat session ends; you can write a follow-up message
     with feedback and the agent re-iterates the plan.
   - **Approve Each Step** (`s`) - agent switches to Standard mode and
     executes the plan, prompting for approval on each action.

In all three cases the stored plan remains as an audit trail. Rejecting a
plan does **not** delete it.

## Fresh session on approval

Planning is read-heavy: the model runs `Read`, `Grep`, and `Tree` across the
codebase before it writes a plan. Once you **Accept** (or **Approve Each
Step**), that exploration is dead weight for execution. So approving a
plan always:

1. Starts a **fresh empty session** (like `/new`) - the planning conversation
   is preserved in storage but no longer in the working context.
2. Adds a hidden user message pointing the agent back at the stored plan
   (`infer plans show <id>`), which it re-reads to execute step by step. The
   plan itself is never lost - it lives in storage and is captured in the
   instruction.

This is simpler and more efficient than summarizing the planning conversation:
the plan is already saved on disk, so there is no need to compact the
conversation. The `compact.enabled` setting is not involved in this flow - it
only controls the *automatic* mid-conversation compaction (see the
[Configuration Reference](configuration-reference.md)).

## Iterating on a plan

Plan mode encourages a back-and-forth before the tool call lands. Typical
flow:

1. You: *"Plan a refactor that splits `chat_handler.go` into smaller
   files."*
2. Model: *"Before I draft this, three questions: …"* (regular assistant
   turn, no tool call yet).
3. You answer.
4. Model: *(maybe more questions)* - repeat until aligned.
5. Model calls `RequestPlanApproval` with the final plan.
6. You **Reject** with a comment ("the split is too granular - keep
   shortcut handling in the same file").
7. Model adjusts and re-submits a new plan (a new file lands in
   `plans/`).
8. You **Accept** - the agent executes.

## Configuration

Plan mode itself has no separate config - it ships with the CLI. You can
customise the system prompt and the tool description in
`.infer/prompts.yaml`:

```yaml
agent:
  system_prompt_plan: |-
    # your custom plan-mode prompt
tools:
  RequestPlanApproval:
    description: |-
      # your custom tool description
```

Empty fields fall back to the in-code defaults (see
`config/prompts.go::DefaultPromptsConfig`). Environment-variable
overrides:

- `INFER_PROMPTS_AGENT_SYSTEM_PROMPT_PLAN`
- `INFER_PROMPTS_TOOLS_REQUEST_PLAN_APPROVAL_DESCRIPTION`

## Related

- [Tools Reference - RequestPlanApproval](tools-reference.md#requestplanapproval-tool)
- [Configuration Reference](configuration-reference.md)
