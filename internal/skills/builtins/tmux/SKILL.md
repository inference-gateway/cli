---
name: tmux
description: >
  Drive interactive terminal programs (TUIs, REPLs, pagers, a debugger, another
  CLI's chat UI) that the Bash tool cannot script directly, by running them
  inside tmux and scripting them with send-keys / capture-pane. Use whenever a
  task needs you to launch, type into, read the live screen of, or tear down an
  interactive terminal program - e2e-testing a TUI, stepping a debugger, or
  answering a prompt a plain command would block on. Not for subagents you
  spawned with the Agent tool (use ReadSubagentScreen / SendSubagentInput for
  those).
license: Apache-2.0
---

# Driving interactive terminal programs with tmux

Plain Bash runs a command and returns once it exits, so it cannot drive a
program that stays open and repaints - a TUI, a REPL, a pager, a debugger, or
another CLI's chat interface. tmux gives you a persistent terminal you can type
into and read back frame by frame.

Run each tmux command as its OWN Bash call. Do not chain with `|`, `&&`, `;` or
add redirects like `2>/dev/null`, `|| true`, or `| grep` - the Bash gate rejects
those. Read a captured frame in a follow-up step instead of piping it.

## 1. Get a terminal - reuse the CURRENT session, add a pane

FIRST find out whether you are already inside tmux. Run this as its own Bash
call and look at the output:

    echo "$TMUX"

**Non-empty output → you are inside the user's tmux session. Add a PANE to it
so the work is visible in the window the user already has open. Do NOT run
`tmux new-session` - a new session is detached and hidden; the user cannot see
it (that is exactly the wrong behavior).** Split the current window:

    tmux split-window -h -P -F '#{pane_id}'

This prints the new pane id (e.g. `%7`); pass it as `-t %7` to every command
below. Use `-h` for a side-by-side split, `-v` for top/bottom, or
`tmux new-window -P -F '#{pane_id}'` for a new tab in the SAME session. Launch a
program directly in the pane by appending it:

    tmux split-window -h -P -F '#{pane_id}' 'htop'

**Empty output → there is no session to join** (a headless, CI, or piped run).
ONLY then create your own detached session with a fixed size (fixed size keeps
captures deterministic):

    tmux new-session -d -s work -x 200 -y 50 'htop'

Target a detached session by name (`-t work`). A bare pane id like `%7` is not
stable across runs, so keep the id from `-P` or the session name as your handle.

When you finish, clean up only what YOU created (see step 4): kill the pane you
split off, or the detached session you started - never kill the user's session.

## 2. Send input

Literal text uses `-l`, so shell metacharacters and words like `Enter` are typed
verbatim instead of being read as key names:

    tmux send-keys -t %7 -l 'print("hello")'

Named keys are sent WITHOUT `-l`, and submit is a SEPARATE call:

    tmux send-keys -t %7 Enter

Common named keys: `Enter`, `Escape`, `Tab`, `BTab` (shift+tab), `Space`,
`BSpace`, `Up`, `Down`, `Left`, `Right`, `C-c` (ctrl+c), `C-d`.

## 3. Read the screen - sleep first

The program repaints asynchronously; an immediate capture shows a stale frame.
Sleep briefly, then capture in the next call:

    sleep 1

    tmux capture-pane -t %7 -p -S -50

`-p` prints the pane to stdout; `-S -50` includes the last 50 scrollback lines.
Inspect the returned text in your following step. If the output you expect is not
there yet, sleep again and re-capture rather than piping into grep.

## 4. Clean up

Tear down only what YOU created, and never the user's session. If you split a
pane in the current session, kill that pane by id:

    tmux kill-pane -t %7

If you started your own detached session (the fallback), kill it by name:

    tmux kill-session -t work

If a program wedges, send `C-c` first, then kill. Leave no orphaned panes or
sessions behind.

## Not for subagents

To drive a subagent you spawned with the Agent tool, use `ReadSubagentScreen`
and `SendSubagentInput` - the CLI already manages those panes for you. Reach for
raw tmux only for programs you launch yourself.

## Approval

tmux is not auto-approved by default: `new-session '<cmd>'` can launch any
program, so each tmux call goes through the normal approval gate unless the
operator has allow-listed it. Expect an approval prompt in chat (or a block in
headless mode) until it is added to `tools.bash.mode.<mode>.allow`.
