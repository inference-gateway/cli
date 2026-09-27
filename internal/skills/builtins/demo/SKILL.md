---
name: demo
description: >
  Record a short demo GIF of a change - a CLI, TUI or desktop GUI app. Use
  when the user types /demo (e.g. "@infer /demo" on a pull request) or asks
  to demo, show or demonstrate a feature, fix or PR, or wants a GIF or
  recording of it working - even as the tail of a bigger task ("implement X
  and demo it"). It plans the demo up front, rehearses it unrecorded,
  records one take and converts it to exactly one GIF in ~/.infer/artifacts.
  Recordings never enter the repository. Not for reproducing bugs (use bug).
license: Apache-2.0
---

# Recording a demo

The user wants to SEE the change working. The deliverable is exactly one GIF,
`~/.infer/artifacts/demo.gif`, from a single take recorded at the end of the
run. In CI, infer-action embeds everything in `~/.infer/artifacts` in the
result comment: that directory IS the delivery channel, so a recording never
needs to go into the repository to survive the runner.

Run every shell command as its OWN Bash call - no `|`, `&&`, `;`, `$(...)` or
redirects; the Bash gate rejects those.

## 1. Plan first

- Add "Record the demo" as the LAST todo now. The recording comes after the
  work is committed and pushed: a run that hits its time or turn limit loses
  unpushed work, but only loses a demo.
- `/demo` on a PR means: demo that PR as it is, do not change code.
  `gh pr view <n>` and `gh pr diff <n>` tell you what changed for the user.
- Asked to build something AND demo it? Keep the build lean and leave a few
  minutes for the demo at the end.
- Pick one to three steps a user would do: run the new flag, open the view
  that changed, click the new button, trigger the message that now reads
  better. Passing tests are not a demo. If nothing shows up on screen
  (refactor, CI config, internal types), do not record - say why in your
  summary.

## 2. Where it runs

    echo "$GITHUB_ACTIONS"

`true` - CI (infer-action). A virtual display (`:99`, 1280x720) shows one
xterm attached to the tmux session `demo` (106x29, cwd = the repository,
memory sync and git prompts off). Record with `{"mode": "screen"}`: the
display holds only what you put on it.

- Terminal programs: use only that session - `tmux send-keys -t demo` and
  `tmux capture-pane -p -t demo`. Do not create sessions, start xterms,
  resize panes or switch clients; those windows are not what gets recorded.
- GUI apps: start them from the demo session with `DISPLAY=:99` in front so
  the window opens on the recorded display, then drive them with the
  Computer tools.

Empty - a local run. Never `mode: screen` here: it captures everything else
the user has open.

- Terminal programs: load the tmux skill, split a pane in the user's
  session, and record with `mode: region` around it (or `mode: window`).
- GUI apps: Screenshot to find the window, drive it with the Computer tools,
  and record with `mode: window` (`app:<name>` or `pid:<n>`).

If RecordStart is not in your tools, recording is off for this run (CI: the
request did not say "demo"; locally: `computer_use.recording.enabled`). Say
so once, never edit config, and show the steps as commands plus captured
output or screenshots instead.

## 3. Rehearse without recording

Every RecordStart/RecordStop pair makes a video, so all trial and error
happens in `capture-pane` or Screenshot, which are free. Never record to
probe geometry or timing: the geometry is fixed above and the rehearsal
tells you the timing.

1. Build with the repo's own command. AGENTS.md, the README or
   the Taskfile/Makefile/package.json say how - and often how to run the app
   without real credentials (a mock or demo mode). Prefer that over real API
   keys. Put fixtures and sample input under `/tmp`, never in the repository.
2. Run the steps exactly as you will in the take, checking the screen after
   each one, until they work. Wait until the app is ready (prompt shown,
   window drawn) before typing. A spinner, a reconnect loop, a `-dirty`
   version or an empty window will look the same in the GIF - fix it now.
   - A state that flashes by (streaming, loading, a queue) is hard to catch
     live: slow the fake backend down rather than racing it.
   - tmux: text goes with `-l` so it is typed verbatim; keys go without it
     (`Enter`, `Tab`, `Down`, `Escape`, `C-c`).
3. Reset to a clean start: quit with the app's own keys or close its window,
   then `tmux send-keys -t demo 'clear' Enter`. Never `pkill -f` or
   `killall`: you are an agent process too, and a broad pattern can match
   your own command line and end the run.

## 4. Record one take

Send the whole take as ONE response of tool calls: RecordStart, then each
step (`tmux send-keys`, or a Click/Type/Key) followed by `sleep 2` (3 for a
busy frame), then RecordStop. Calls in one response run in order with no
model turn between them; spread over turns, each step costs ~10 s and the
60 s cap hits before the interesting frame. Aim for 20-45 s.

RecordStop names the MP4 under `~/.infer/tmp/recordings/` and reports
`"capped": true` if it hit 60 s. To check the take, pull one frame and look at
the PNG:

    ffmpeg -loglevel error -y -ss 10 -i <mp4> -frames:v 1 /tmp/demo-frame.png

Never open the MP4 or a GIF with image tools - an animated GIF sent to the
model fails the whole run. A bad take (capped, wrong screen)? Rehearse the
broken step again, then record a new take; unconverted takes stay in
`~/.infer/tmp/recordings` and never reach the comment.

## 5. Convert the good take

    ffmpeg -loglevel error -y -i <mp4> -vf "fps=10,split[a][b];[a]palettegen[p];[b][p]paletteuse" ~/.infer/artifacts/demo.gif

- Always the same name: converting again replaces the GIF instead of adding
  a second one.
- `-ss <seconds>` before `-i` trims dead time at the start.
- `ls -l` it: GitHub shows images up to 10 MB. Bigger? Put `scale=800:-1,`
  in front of `split` and drop to `fps=8`.
- `ffmpeg` not on PATH? It is in `~/.infer/bin/tools/`. Locally,
  `mkdir -p ~/.infer/artifacts` first.

## Rules

- Recordings never enter the repository: do not copy, link, `git add`,
  commit or push an MP4 or GIF.
- Never type or show secrets, tokens, `.env` files or environment variables
  on the recorded screen.
- Do not save memories about how demos work. This skill is the source of
  truth, and what a run without recording enabled observes (no tmux, no
  RecordStart) is wrong for the next run.
- In CI only the text after your last tool call reaches the result comment:
  once the GIF is written, end with the full summary - what the demo shows,
  one line per step.
