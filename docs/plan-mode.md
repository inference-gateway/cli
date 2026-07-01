# Plan Mode

[← Back to README](../README.md)

Plan Mode is a read-only operating mode for the agent. The model can use
`Read`, `Grep`, `Tree`, and `TodoWrite` to investigate the codebase, but it
cannot write, edit, delete, or run shell commands. Instead, it produces a
**plan** for the proposed change, persists that plan as a Markdown file
under `<configDir>/plans/`, and surfaces it to you for approval before any
real work happens.

## Why use it

Plan mode exists for tasks where you want a human gate between
"understand the change" and "make the change":

- Risky or far-reaching refactors.
- Migrations and schema changes.
- Multi-file features where you want a written record before execution.
- Pair-programming flows: review the plan, push back, iterate.

Every plan - accepted **or** rejected - is left on disk in
`<configDir>/plans/` as an audit trail. You can grep, share, or revisit
them later.

## How to enter plan mode

In the interactive chat TUI, press **Shift+Tab** to cycle the agent mode.
The status line shows the current mode; cycle until it reads
**Plan Mode**. Press Shift+Tab again to leave.

The cycle is:

```text
Standard → Plan Mode → Auto-Accept → Standard → …
```

When a plan is **accepted**, the mode automatically flips back to Standard
(or to Auto-Accept if you chose "Accept & Auto-Approve") and the agent
begins executing.

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

```text
<configDir>/plans/<YYYY-MM-DD-HHMMSS>-<slug>.md
```

`<configDir>` is whatever
`config.Config.GetConfigDir()` resolves to - usually `./.infer/` in a
project, or `~/.infer/` for the userspace config. The `plans/`
subdirectory is created on first use.

`<slug>` is the title lowercased, with non-alphanumerics collapsed to
`-`, capped at 60 characters. If two plans land in the same second with
the same title, the second filename gets a numeric suffix (`-1`, `-2`, …)
so writes never clobber each other.

Writes are atomic: the tool writes to `<path>.tmp` and then `os.Rename`s,
so you'll never see a half-written plan file.

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

   - **Accept** - agent flips to Standard mode and executes the plan.
   - **Accept & Auto-Approve** - agent flips to Auto-Accept (no per-tool
     approval prompts) and executes the plan.
   - **Reject** - chat session ends; you can write a follow-up message
     with feedback and the agent re-iterates the plan.

In all three cases the plan file remains on disk. Rejecting a plan does
**not** delete it.

## Context compaction on approval

Planning is read-heavy: the model runs `Read`, `Grep`, and `Tree` across the
codebase before it writes a plan. Once you **Accept** (or **Accept &
Auto-Approve**), that exploration is dead weight for execution. So approving a
plan always:

1. Summarizes the planning conversation into a short `--- Context Summary ---`.
2. Saves the planning session and continues in a **fresh session** seeded with
   that summary - so execution starts with an optimized, much smaller context
   window.
3. Points the agent back at the saved plan file (`<configDir>/plans/…`), which
   it re-reads to execute step by step. The plan itself is never lost - it lives
   on disk and is captured in the summary.

This happens regardless of the `compact.enabled` setting - that flag only
controls the *automatic* mid-conversation compaction (see the
[Configuration Reference](configuration-reference.md)). Compaction on approval
fails open: if summarization errors or times out, execution still proceeds on
the un-compacted conversation.

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
