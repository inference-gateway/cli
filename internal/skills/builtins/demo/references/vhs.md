# Recording terminal demos with VHS

[VHS](https://github.com/charmbracelet/vhs) renders a scripted terminal in headless Chrome and encodes it with
ffmpeg. The frame size is exact and nothing else on the user's screen can leak in. It records terminal programs
only, never GUI apps.

## Rehearse

- Start your own detached tmux session sized like the tape, drive it with `send-keys` / `capture-pane`, and kill
  it when done: `tmux new-session -d -s rehearsal -x 80 -y 39 -c <dir>`. A 1080x1080 tape at FontSize 20 is about
  80 columns by 39 rows.
- Fixtures go under `/tmp/<demo>/`. The tape and its takes go in a directory of their own, such as
  `/tmp/<demo>/takes/`, never in the repository.

## Tape template

```tape
Set Shell "bash"
Set FontSize 20
Set Width 1080
Set Height 1080
Set Padding 24
Set Margin 28
Set MarginFill "#7aa2f7"
Set WindowBar Colorful
Set BorderRadius 12
Set Theme "TokyoNight"
Set TypingSpeed 28ms
Set Framerate 30

Hide
Type `cd /tmp/demo/app && export PATH=/tmp/demo/bin:$PATH PS1='\[\e[1;35m\]app\[\e[0m\] \$ ' PS2='  ' && clear`
Enter
Sleep 1s
Show

Type "my-app --new-flag"
Enter
Wait+Screen@20s /Ready/
Sleep 2s
```

- 1080x1080 (1:1) suits LinkedIn and X. Use 1280x720 for a pull request.
- Full-screen TUIs can clip their borders below 80 columns, so drop the FontSize before narrowing the frame.
- `Set FontFamily "<name>"` picks an installed monospace font. `vhs themes` lists the theme names.
- `PS2='  '` turns heredoc continuation lines into a plain indent, so a file typed on camera with
  `cat > file <<EOF` reads cleanly.
- The `&&` chain lives inside the tape, which VHS types into its own shell. The Bash gate only sees the `vhs` command.

## Gotchas

- Leave `Output` out of the tape and pass the take's absolute path to `-o`:
  `vhs /tmp/<demo>/takes/demo.tape -o /tmp/<demo>/takes/take.mp4`. Every Bash call starts in the repository and the
  gate rejects `cd … &&`, so a relative `Output` would land in the repository, and an absolute one inside the tape
  fails to parse.
- Leave `Screenshot` out too: its path resolves against the repository as well, and one as the tape's last command
  is never written. Check a take by pulling frames from the MP4 instead, with `-ss 12` for second 12 or
  `-sseof -0.2` for the last frame:
  `ffmpeg -loglevel error -y -sseof -0.2 -i take.mp4 -frames:v 1 /tmp/demo-frame.png`
- Text that contains double quotes goes in backticks: ``Type `!!Tool(arg="x")` ``.
- `Wait+Screen@<timeout> /regex/` waits out real, variable latency such as an LLM call. Wait for text that only
  appears once the step is done. A status line left over from the previous step matches at once.
- The VHS shell inherits the environment of the process that runs `vhs`, not the user's terminal. `printenv` the
  app's overrides (for infer, `INFER_GATEWAY_MOCK` and friends) and `unset` the unwanted ones in the hidden setup.
  A tmux rehearsal runs with the tmux server's environment, so a take can behave differently from it.
- Hold key frames 2-3 s with `Sleep` and the last frame about 4 s. At 28ms per character, typing runs about 35
  characters a second.
- Output from a real model varies between takes. Record a second take when the first reads badly, compare their
  last frames, and convert only the one you keep.

## Demoing infer chat with a real model

- `INFER_AGENT_MODEL=<provider/model>` picks the model. Provider keys load from `~/.infer/auth.yaml` or the
  project's `.env` by themselves, so never type or print them.
- `INFER_PLUGINS_ENABLED=false` keeps the user's plugins out of the visible thinking, and
  `INFER_AGENT_REASONING_EFFORT=minimal` shortens it.
- A long final answer pushes the tool call off the frame. An `AGENTS.md` in the fixture project that says "Keep
  every reply to one short sentence." prevents that, though the thinking may mention the file.
- `!!Tool(...)` counts as the user's approval and runs at once. A call the model makes shows the approval box, and
  Enter approves it.
