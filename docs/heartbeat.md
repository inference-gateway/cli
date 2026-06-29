# Heartbeat Guide

[← Back to README](../README.md)

The **Heartbeat** wakes the agent on a fixed interval to check for
pending work without waiting for user input. Unlike the
[Schedule](./scheduling.md) tool - which lets the LLM create
user-driven cron jobs that deliver output to a channel - the
heartbeat is a global self-driven tick the operator configures once.

It exists for autonomy use cases: "every hour, look at my todos and
make progress on whatever is next." The agent decides what to do at
each tick using a tailored system prompt.

## How it works

```text
┌───────────────────────────────────────────────────────────────┐
│  infer channels-manager (long-running daemon)                 │
│                                                               │
│   ├─ ChannelManagerService    (channels - optional)           │
│   ├─ SchedulerService         (cron jobs - optional)          │
│   └─ HeartbeatService                                         │
│        ├─ time.Ticker(interval)                               │
│        └─ on tick: spawn `infer agent --heartbeat             │
│                                  --session-id <uuid> <prompt>`│
│                    log stdout                                 │
└───────────────────────────────────────────────────────────────┘
                                       ▲
                                       │ reads on startup
                          ┌────────────┴────────────────┐
                          │ .infer/heartbeat.yaml       │
                          │ .infer/prompts.yaml         │
                          │   (system_prompt_heartbeat) │
                          └─────────────────────────────┘
```

Key properties:

- **Off by default.** Heartbeat is opt-in via the `enabled` flag in
  `heartbeat.yaml`.
- **Single global instance.** One interval, one prompt - for
  multi-job use cases use the [Schedule tool](./scheduling.md)
  instead.
- **Daemon-bound.** Heartbeat only fires while `infer
  channels-manager` is running. If the daemon is down, ticks are
  skipped (no replay).
- **Fresh session per tick.** Each tick gets a new UUID session ID;
  no context carries between fires. The agent inspects persistent
  state (todos, scheduled jobs, conversation history) to decide what
  to do.
- **Overlap-safe.** If a tick takes longer than the interval, the
  next tick is skipped rather than running concurrently.
- **Output to logs.** The agent's stdout is logged. Whatever
  externally-visible action you want the agent to take (post to
  Telegram, open a PR, run a job) it does via its own tools - the
  heartbeat is just the trigger.

## Setup

### 1. Generate the config files

```bash
infer init
```

This creates `.infer/heartbeat.yaml` (disabled by default) and
includes a `system_prompt_heartbeat` entry in `.infer/prompts.yaml`.

### 2. Enable and tune the heartbeat

Edit `.infer/heartbeat.yaml`:

```yaml
---
enabled: true
interval: 1h            # how often to wake (Go duration: 30s, 5m, 1h, 24h)
initial_delay: 1m       # delay before first tick (avoids fire-on-start)
model: ""               # optional override; empty = agent.model from config.yaml
prompt: "Heartbeat tick - check for any pending tasks, todos, or background work and act on them."
```

The `prompt` field is the **user message** sent to the agent each
tick. The **system prompt** (the steering instructions) lives in
`.infer/prompts.yaml`:

```yaml
agent:
  system_prompt_heartbeat: |
    You are an autonomous agent that has just been woken up by a
    periodic heartbeat tick.
    ...
```

The default heartbeat system prompt is conservative - it tells the
agent to check pending todos and background tasks, take at most one
concrete step per tick, and exit. Override it to fit your workflow.

### 3. Run the daemon

```bash
infer channels-manager
```

The daemon hosts up to three subsystems - channels, scheduler, and
heartbeat - and starts whichever are enabled. You can run with
heartbeat alone (no channels, no scheduler) if that is all you need.

```text
INFO Starting channels-manager
INFO Heartbeat service started  interval=1h0m0s  initial_delay=1m0s
INFO Daemon ready. Press Ctrl+C to stop.
```

When a tick fires:

```text
INFO Heartbeat tick - spawning agent  session_id=…  model=
INFO Heartbeat agent output  session_id=…  line={"role":"assistant","content":"…"}
INFO Heartbeat tick complete  session_id=…
```

## Configuration reference

### `.infer/heartbeat.yaml`

| Field           | Type            | Default     | Description                                                                                |
| --------------- | --------------- | ----------- | ------------------------------------------------------------------------------------------ |
| `enabled`       | bool            | `false`     | Feature flag. Heartbeat is opt-in.                                                         |
| `interval`      | duration string | `"1h"`      | How often to fire. Parsed via `time.ParseDuration` (e.g. `30s`, `5m`, `1h`, `24h`).        |
| `initial_delay` | duration string | `"1m"`      | Delay before the first tick after the daemon starts. Set to `"0s"` to fire immediately.    |
| `model`         | string          | `""`        | Optional model override. Empty falls back to `agent.model` in `config.yaml`.               |
| `prompt`        | string          | (built-in)  | The user message sent to the agent each tick.                                              |

### Environment variables

Mirroring the file fields. Env vars win over `heartbeat.yaml`.

| Variable                                      | Maps to                              |
| --------------------------------------------- | ------------------------------------ |
| `INFER_HEARTBEAT_ENABLED`                     | `enabled`                            |
| `INFER_HEARTBEAT_INTERVAL`                    | `interval`                           |
| `INFER_HEARTBEAT_INITIAL_DELAY`               | `initial_delay`                      |
| `INFER_HEARTBEAT_MODEL`                       | `model`                              |
| `INFER_HEARTBEAT_PROMPT`                      | `prompt`                             |
| `INFER_PROMPTS_AGENT_SYSTEM_PROMPT_HEARTBEAT` | the system prompt in `prompts.yaml`  |

## Common patterns

### Hourly TODO sweep

```yaml
# heartbeat.yaml
enabled: true
interval: 1h
prompt: "Sweep open todos. If any are stale (>24h with no progress), pick the highest-priority one and take the next concrete step."
```

### Build / CI watchdog

```yaml
# heartbeat.yaml
enabled: true
interval: 15m
prompt: "Check the status of the latest GitHub Actions run on this repo. If it failed, summarise the failure in the conversation log."
```

The agent inspects GitHub state with the `gh` CLI via the Bash tool - there is no
built-in GitHub tool. Read-only `gh` subcommands (`gh run view`, `gh pr list`, ...)
are on the standard bash allow-list; keep the prompt scoped to reporting and warn
against opening issues or pushing changes automatically.

## Troubleshooting

**Heartbeat never fires** - confirm `enabled: true` and that
`infer channels-manager` is running. The daemon logs `Heartbeat
service started` on boot when it picks up the config.

**Heartbeat fires too often / not enough** - check `interval`
parses correctly. Bad durations (`1H`, `30 minutes`) cause the
daemon to fail-fast on startup with a clear error.

**Agent does nothing** - heartbeat works; the agent decided no
action was needed. Check the system prompt - the default explicitly
tells it to no-op when nothing is pending.

**"Heartbeat tick skipped - previous run still in flight"** - your
agent is taking longer than `interval` to complete. Either increase
the interval, simplify the prompt, or set a tighter `agent.max_turns`
in `config.yaml`.

## Comparison with the Schedule tool

|                | Heartbeat                    | Schedule tool                        |
| -------------- | ---------------------------- | ------------------------------------ |
| Configured by  | Operator (yaml file)         | LLM (via tool calls)                 |
| Multiplicity   | Single global tick           | Many user-defined jobs               |
| Trigger        | Fixed interval (Go duration) | Cron expression (per job)            |
| Output         | Logs                         | A messaging channel (Telegram, …)    |
| Use case       | Autonomous self-monitoring   | "Remind me at 8am tomorrow"          |

The two are complementary and can run side by side in the same
daemon.
