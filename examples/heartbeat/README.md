# Heartbeat

The [heartbeat](../../docs/heartbeat.md) wakes the agent on a fixed interval to check for pending work,
without waiting for user input. This example shows the two ways to run it, offline against the scripted
mock model:

- `task daemon` - the real thing: `infer daemon` reads `heartbeat.yaml`, wakes the agent every interval,
  and each wake-up is recorded as a conversation in storage you can read back
- `task once` - a single wake-up without a daemon: `infer headless --heartbeat "<prompt>"`, which swaps in
  the heartbeat system prompt for one run

The model is a [tokenless](https://github.com/inference-gateway/tokenless) mock scripted in
[`scenarios.yaml`](scenarios.yaml), so the example runs with no API key and no gateway. You need
[Task](https://taskfile.dev).

## Setup

`task home` copies [`heartbeat.yaml`](heartbeat.yaml) into the demo HOME as `~/.infer/heartbeat.yaml` - the
file the daemon reads:

```yaml
enabled: true
interval: 30s
initial_delay: 5s
prompt: Heartbeat tick - check pending work and act on what is next.
```

`enabled: false` is the default, so the heartbeat only runs once you switch it on - here it is on, with a
short interval so the demo ticks quickly, and `initial_delay` so the first wake-up lands five seconds after
the daemon starts instead of firing on start. Every environment variable mirrors a file field
(`INFER_HEARTBEAT_ENABLED`, `INFER_HEARTBEAT_INTERVAL`, `INFER_HEARTBEAT_INITIAL_DELAY`,
`INFER_HEARTBEAT_MODEL`, `INFER_HEARTBEAT_PROMPT`) and wins over the file.

The `prompt` is the user message of each wake-up. The steering instructions are a system prompt -
`system_prompt_heartbeat` in `~/.infer/prompts.yaml`, which `infer init` seeds and which tells the agent to
check pending todos and background work, take at most one concrete step, and exit. Override it to fit your
workflow. `infer headless --heartbeat` uses the same system prompt, which is what makes `task once` a
heartbeat run.

## Layout

```text
heartbeat/
├── Taskfile.yml    # one task per scenario, with the mock model's environment
├── heartbeat.yaml  # the heartbeat config, copied into the demo HOME by `task home`
├── scenarios.yaml  # what the mock model says when a wake-up lands
└── .home/          # the demo HOME (ignored by git)
    └── .infer/
        ├── heartbeat.yaml
        └── conversations.jsonl
```

## Run it

```bash
task daemon    # keep it in its own shell
```

The daemon logs `heartbeat service started interval=30s initial_delay=5s`, and five seconds later:

```text
heartbeat tick - spawning agent conversation_id=…
heartbeat agent output conversation_id=… line={"role":"assistant",...}
heartbeat tick complete conversation_id=…
```

That repeats every 30 seconds. Ctrl+C stops it. `task wakes` lists the conversations the ticks recorded -
one per wake-up, because every tick gets a fresh session with no context carried over - and
`task show ID=<paste an ID from the list>` prints the wake-up prompt and the reply. `task clean` removes
the demo HOME.

## Tasks

| Task | What happens |
| --- | --- |
| `task daemon` | Runs `infer daemon` with the heartbeat on. The heartbeat only fires while it runs |
| `task wakes` | `infer conversations list`, one conversation per wake-up |
| `task show ID=...` | `infer conversations show`, the full wake-up transcript |
| `task once` | `infer headless --heartbeat "<prompt>"` - one wake-up without a daemon |
| `task clean` | Removes the demo HOME |

## What to expect

`task once` runs one tick straight away - the heartbeat system prompt is in effect, the mock model replies
scripted:

```text
{"role":"assistant","content":"Nothing pending: no unfinished todos and no background jobs. Ending this tick idle."}
```

`task wakes` after a couple of minutes of `task daemon` - the table lists the wake-up conversations (their
IDs print in the daemon log as `conversation_id=…`), while `task show ID=<id>` shows the prompt
`Heartbeat tick - check pending work and act on what is next.` and the reply above.

## Use a real model

Drop `INFER_GATEWAY_MOCK`, `INFER_GATEWAY_MOCK_SCENARIOS` and `INFER_AGENT_MODEL` and the wake-ups run on
your real provider: at every tick the agent sees the heartbeat system prompt plus the prompt, checks its
actual todos and background jobs, and acts or stays idle. The heartbeats complement the
[Schedule tool](../../docs/scheduling.md) - one operator-configured tick versus many user-created cron jobs
(see the offline [scheduling example](../scheduling/)).
