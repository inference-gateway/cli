# Reminders & command hooks

Two extension points fire at fixed points of the agent loop without touching the system prompt or forking the agent:

- **Command hooks** (`.infer/hooks.yaml`) run a shell command - the executable sibling of reminders.
- **Reminders** (`.infer/reminders.yaml`) inject a `<system-reminder>` text block - the non-executable sibling of hooks.

This example hooks `post_session` (the loop is finishing), which writes its session context to a log file, and adds one custom reminder. The model is
a [tokenless](https://github.com/inference-gateway/tokenless) mock scripted in [`scenarios.yaml`](scenarios.yaml), so the demo runs offline with no
API key and no gateway. You need [Task](https://taskfile.dev).

See [Reminders & Command Hooks](../../docs/hooks.md) and the [configuration reference](../../docs/configuration-reference.md) for every hook
point and trigger.

## Layout

```text
hooks/
├── Taskfile.yml                   # one task per demo, with the mock model's environment
├── scenarios.yaml                 # what the mock model says, per prompt
├── swap-reminders.yaml            # an alternative reminder set, fed in with --reminders-file
└── .infer/
    ├── hooks.yaml                 # one post_session command hook, enabled: true
    └── reminders.yaml             # one custom reminder with merge: true over the built-ins
```

## Run it

| Task | What happens |
| --- | --- |
| `task hook` | Runs a session with the hook command on the bash allow-list - at `post_session` the hook runs and `hook-runs.log` gains a line |
| `task hook:skipped` | Runs the same session but leaves the command off the allow-list. The hook is skipped, nothing is run, nothing is written |
| `task reminder` | Runs with `.infer/reminders.yaml` - `merge: true` lets one custom reminder join the built-ins, visible in the verbose stream |
| `task reminder:swap` | `--reminders-file swap-reminders.yaml` replaces the whole set - merged reminder and built-ins gone, the swap entry fires |
| `task chat` | Opens `infer chat` with the mock model, so you can chat and watch `hook-runs.log` after each finished session |
| `task clean` | Removes the files the hook writes |

The tasks use the `infer` on your `PATH`. To use a local build instead, run `task build` in the repository root and pass it
in, for example `task hook INFER=../../infer`.

### What to expect

`task hook` - with `INFER_TOOLS_BASH_ALLOW_APPEND` naming the hook's command, the command runs when the session ends:

```text
{"type":"hook_command", ... "name":"session-end-marker", "hook":"post_session", "exit_code":0, "output":"{\"hook\":\"post_session\",...}", ...}

hook-runs.log:
{"hook":"post_session","session_id":"a66338a6-4893-4de9-9a64-a3a1de721f0f","turn":1}
```

The hook receives the session context on stdin, so the `tee hook-runs.log` command leaves the payload in the log. That file is
committed as `hook-runs.log` in this directory, from an actual run.

`task hook:skipped` - the same `hooks.yaml`, but the command is not on the allow-list. The hook is reported, not run:

```text
{"type":"hook_command_skipped", ... "name":"session-end-marker", "hook":"post_session", "reason":"not_allowlisted", ...}

no hook-runs.log - the command was skipped, never run
```

`task reminder` - the custom reminder fires next to the built-in ones:

```text
{"role":"user", ..., "kind":"system_reminder", "name":"conventions-nudge", "content":"<system-reminder>Verify the answer ...</system-reminder>"}
```

`task reminder:swap` - the file swaps the set:

```text
{"role":"user", ..., "kind":"system_reminder", "name":"focus-checkpoint", "content":"<system-reminder>Swap checkpoint: ..."}
conventions-nudge is gone - the file replaced the whole set
```

## The allow-list is part of the contract

A command hook is a shell command the agent runs without you proposing it, so it faces the same gate as every model-proposed command: the per-mode
bash allow-list. A hook that is `enabled: true` but not allow-listed is skipped and reported on every fire, exactly what `task hook:skipped` shows.
Allow it, either in `tools.yaml` or with the environment:

```bash
INFER_TOOLS_BASH_ALLOW_APPEND="tee hook-runs.log" infer chat
```

The `hook` field is one of `pre_session`, `pre_stream`, `post_stream`, `pre_tool`, `post_tool`, `pre_queue_drain`, `post_queue_drain`, `post_session`.

## Reminders: merge and swap

`reminders.yaml` ships **enabled** with nine built-in defaults. Two knobs decide what a file of yours does:

- `merge: true` - your entries join the built-ins (same `name` replaces one). That is `.infer/reminders.yaml` in this example.
- No `merge` (or `merge: false`) - your entries replace the built-ins wholesale. That is `swap-reminders.yaml`, and `--reminders-file` selects it:
  `infer --reminders-file swap-reminders.yaml headless "say hello"`.

The flag beats the project file: precedence is `INFER_REMINDERS_CONFIG`, then `--reminders-file`, then `.infer/reminders.yaml`, then
`~/.infer/reminders.yaml`. The verbose stream shows every reminder as a hidden system-reminder message tagged with its name - that is what the
`task reminder` tasks grep for. Because a set `INFER_REMINDERS_CONFIG` beats every file, those tasks clear it (`INFER_REMINDERS_CONFIG=`)
before running, so an exported override - some CI runners ship one - cannot shadow the demos.

## Without Task

The Taskfile only sets four environment variables. Set them yourself to run `infer` directly from this directory:

```bash
export INFER_GATEWAY_MOCK=true
export INFER_GATEWAY_MOCK_SCENARIOS="$PWD/scenarios.yaml"
export INFER_AGENT_MODEL=mock/openai/gpt-4o
export INFER_STORAGE_ENABLED=false

INFER_TOOLS_BASH_ALLOW_APPEND="tee hook-runs.log" infer headless --no-save --verbose "say hello"
cat hook-runs.log
```

## Use a real model

Drop `INFER_GATEWAY_MOCK`, `INFER_GATEWAY_MOCK_SCENARIOS` and `INFER_AGENT_MODEL` to talk to a real model through the gateway. Hooks and
reminders are model-independent: both fire around the loop, not inside it.
