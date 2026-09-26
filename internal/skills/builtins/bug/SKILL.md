---
name: bug
description: >
  Turn a rough bug report into a reproduced, well-formed GitHub issue. Use
  when the user types /bug <context> (e.g. /bug the app opens a blank window
  on startup) or asks to reproduce and report a bug: it gathers details,
  reproduces the bug in a scratch directory, optionally records a window or
  region GIF, checks for duplicates, and files a [BUG] issue only after the
  user approves. Built-in and CLI-specific; not for feature requests or
  general support questions.
license: Apache-2.0
---

# Reproducing and reporting a bug

The user types `/bug <context>` because reporting is a chore; your job is to
make it lazy on their side: gather just enough context, reproduce the bug,
optionally record it, and draft the issue. Nothing is ever posted without
the user's explicit approval.

Declining anything is fine and expected: a declined step degrades the report
(text-only instead of GIF, draft instead of filed issue) - it never aborts
the skill.

Run every shell command as its OWN Bash call - no `|`, `&&`, `;`, `$(...)` or
redirects like `2>/dev/null`; the Bash gate rejects those. Capture values a
command prints and paste them literally into later calls.

## 1. Gather

Start from `<context>` - it is the seed, not the full story. Ask follow-ups
with AskUserQuestion in ONE call of up to 4 questions:

- What were you doing when it happened? (the trigger)
- What did you expect to happen, and what happened instead?
- How often does it happen? (every time / sometimes / one-off)
- Which OS, terminal and `infer version`? (if not already known)

Rules:

- Every question MUST include a `Just reproduce it` option (2-4 options per
  question; the UI adds an `Other` free-text choice automatically). Picking
  it - in any question - ends questioning immediately: move on with what you
  know and never open another question round for this bug.
- Do not spend an option slot on anything `<context>` already answers.
- If the tool result reports no interactive user (`available: false`),
  ignore its stop instruction and do not restate the questions in plain
  text: this skill is expected to run without a host. Proceed straight to
  reproducing with what you have.

## 2. Reproduce

Work in a scratch directory so the user's project is not modified:

    mktemp -d

(keep the printed path; paste it literally into later calls - `$(...)` is
forbidden).

- Terminal / TUI bugs: load the tmux skill (read its `SKILL.md` - the path
  is in your AVAILABLE SKILLS list) and drive the program there. The infer
  CLI's own chat UI is a TUI; `INFER_GATEWAY_MOCK=true` exercises it without
  a real LLM.
- GUI bugs: use the Computer tools - `Screenshot` first to find the affected
  window, then Click / Type / Key to drive it.

Reproduce the bug at least twice before you trust the recipe. While you do,
write down the MINIMAL step list that triggers it - that list becomes
"Steps to Reproduce". Capture environment facts too: the output of
`infer version` and of `uname -a` (paste them into the Summary later).

If you cannot reproduce it after an honest effort, do not fake it: say so,
document what you tried, and continue - the report asks for the missing
detail instead of pretending.

## 3. Record (opt-in - only after it reproduces)

Recording is OFF by default (`computer_use.recording.enabled` in
`~/.infer/computer_use.yaml`, max 120 seconds). If RecordStart is missing
from your available tools, say so once - with the config key that enables
it - and continue text-only. Never edit the config yourself.

On consent (one AskUserQuestion: record now / text-only; `Just reproduce it`
counts as no):

1. NEVER record with `mode: screen`. Use `mode: window` scoped to the
   affected app (`frontmost`, `app:<name>` or `pid:<n>`) or `mode: region`
   around it.
2. Before RecordStart, take a Screenshot of the target window and check it
   shows no secrets - see the guardrails below. When in doubt, do not
   record; file a text-only report.
3. Rehearse the steps once so the replay fits
   `computer_use.recording.max_duration` (120s default). Then RecordStart,
   replay the minimal steps, RecordStop.
4. The RecordStop result names the MP4 (written under
   `~/.infer/tmp/recordings/` by default). Convert it to a GIF with ffmpeg
   (the recorder resolves ffmpeg itself, with a `~/.infer/bin` fallback):

       ffmpeg -i <mp4> -vf fps=12,scale=800:-1:flags=lanczos <gif>

5. Keep it under GitHub's 10 MB image limit: check with `ls -l`, and if over
   re-encode with a lower fps (10), a narrower scale (600), or trimmed ends
   (`-ss` / `-t`).
6. Show the user the GIF's local path for review. Never upload it anywhere
   yourself.

If the capture fails, ffmpeg is missing, or the GIF comes out unusable, say
so once and fall back to a text-only report.

## 4. File

1. Target repo: `inference-gateway/cli` by default (the skill is
   CLI-specific). If the bug clearly belongs to another repository - the
   user named it, or it is the repo in the working directory - use that
   instead and state which repo you picked.
2. Duplicates first:

       gh issue list --repo <target> --search "is:open <keywords>" --limit 5

   Show the likely matches. If one is clearly the same bug, tell the user
   and let them decide between a new issue and adding to the existing one.
3. Draft the issue exactly on the org template: title `[BUG] <one line>`,
   label `bug`, and four sections - `## Summary` (include the `infer
   version` output and the OS), `### Steps to Reproduce` (your minimal step
   list), `### Expected Behavior`, `### Actual Behavior`. Redact secrets and
   private paths (guardrails below).
4. Write the body to a file, then show the title, the body file path and the
   GIF path, and ask for consent (AskUserQuestion: file it / I will edit it
   first / do not file). No consent means you stop with the draft - you
   never post anything.
5. GitHub has no issue-attachment API and `gh` cannot upload files, so v1
   hands off to the browser - which doubles as the final review step:

       gh issue create --repo <target> --web --title "[BUG] <one line>" --body-file <draft.md> --label bug

   The browser opens with the form prefilled; the user drags the GIF in from
   the printed path and submits it themselves.

When the report is filed (or handed back), clean up the scratch directory.

## Privacy guardrails (non-negotiable)

- Never record with `mode: screen` - only the window or region of the app
  under test.
- Before RecordStart, screenshot the target window and check for secrets:
  env vars, `.env` or config files, tokens / API keys, password managers,
  email or chat apps, notifications. When in doubt, do not record - file a
  text-only report.
- Never type real credentials while recording; use placeholders.
- Redact tokens, keys, emails, hostnames and home-directory paths from any
  log or output pasted into the issue (`/home/<user>/...` becomes `~/...`).
- The user reviews the GIF (local path printed) and the issue body before
  anything is posted. Declining consent at any point means a text-only
  report, not an aborted one.

## Fallbacks

Recording disabled, ffmpeg missing, a failed capture, an unreproducible bug
or a declined consent - none of these fail the skill. They degrade to a
text-only report: file it with consent, or hand the draft and the exact
`gh issue create --web` command back to the user.

## Approval

ffmpeg and `gh` are not auto-approved by default: each call goes through the
normal approval gate unless the operator allow-listed it, which doubles as a
safety net. Expect prompts in chat (or blocks in headless) until those
commands are added to `tools.bash.mode.<mode>.allow`.
