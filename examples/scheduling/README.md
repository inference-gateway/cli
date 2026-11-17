# Scheduled tasks

The [Schedule tool](../../docs/scheduling.md) lets the model create cron jobs. This example runs the whole
loop offline: the mock model calls `Schedule` with `operation: create`, `infer daemon` picks the job up from
storage within seconds, fires it on the cron expression, and records every fire. Nothing carries between
fires - each one runs the job's prompt in a fresh session and saves it as a conversation in storage, which
is how record-only jobs (everything not created from a chat channel) are read back.

The model is a [tokenless](https://github.com/inference-gateway/tokenless) mock scripted in
[`scenarios.yaml`](scenarios.yaml), so the example runs with no API key and no gateway. You need
[Task](https://taskfile.dev).

## Setup

Every `infer` command runs with four environment variables, set inline on the command:

- `INFER_TOOLS_SCHEDULE_ENABLED=true` - turns the Schedule tool on (off by default, see
  [Scheduling](../../docs/scheduling.md#1-enable-the-tool))
- `INFER_GATEWAY_MOCK`, `INFER_GATEWAY_MOCK_SCENARIOS`, `INFER_AGENT_MODEL` - the mock model

Inline, because a calling shell that already exports one of these (a CI runner driving `infer` exports
`INFER_AGENT_MODEL`) would otherwise win over anything a variable block sets. The `HOME` prefix points at
`.home`, a demo HOME the tasks keep seeded - `task home` drops the hand-written job into it by hand. Under
it the jsonl storage backend keeps jobs in `~/.infer/schedules/<job id>.yaml` - one file per job,
hot-reloaded by the daemon - and run records next to them under `.infer/schedules/runs/`.

## Layout

```text
scheduling/
├── Taskfile.yml                  # one task per scenario, with the mock model's environment
├── scenarios.yaml                # what the mock model says, for the tool calls and for the fires
├── schedules/
│   └── demo-standup-summary.yaml # a hand-written job, seeded into the demo HOME by `task home`
└── .home/                        # the demo HOME (ignored by git)
    └── .infer/
        └── schedules/            # one YAML file per job, plus runs/ for the run records
```

`schedules/demo-standup-summary.yaml` is a job written by hand, the way you would drop one in yourself. The
daemon's two-second poll picks it up exactly like a job the tool created - `task delete` removes it again
through the tool.

## Run it

Create two jobs through the mock model, then look at what the tool wrote:

```bash
task create:quote     # Schedule create, recurring: cron_expression '@every 1m'
task create:reminder  # Schedule create with run_once: true - fires once, then deletes itself
task list             # Schedule list - both jobs plus the seeded one, with their IDs
task jobs             # the job files written to ~/.infer/schedules/
```

Now start the daemon in its own shell:

```bash
task daemon
```

It logs `scheduler started jobs=3`, and about a minute later `firing scheduled job` twice. The quote job
fires every minute with a fresh session ID; the reminder fires once and the daemon logs
`one-off scheduled job consumed and deleted`. Back in the first shell, read the fires that were recorded:

```bash
task runs              # infer conversations list - one conversation per fire
task show ID=<paste an ID from the list>  # infer conversations show, the full fire transcript
```

While the daemon runs (hot reload means no restart needed):

```bash
task delete   # Schedule delete on the seeded job - the daemon logs "scheduled job removed"
task clean    # Ctrl+C the daemon first: removes the demo HOME
```

## Tasks

| Task | What happens |
| --- | --- |
| `task create:quote` | The mock model calls `Schedule` with `operation: create` and `cron_expression: '@every 1m'` |
| `task create:reminder` | The same, with `run_once: true` |
| `task list` | The mock model calls `Schedule` with `operation: list` |
| `task delete` | The mock model calls `Schedule` with `operation: delete` on the seeded job's ID |
| `task jobs` | Prints the job files and any run records under `~/.infer/schedules/` |
| `task home` | Seeds the demo HOME, re-copying the hand-written job into `~/.infer/schedules/` |
| `task daemon` | Runs `infer daemon`; jobs only fire while it runs |
| `task runs` / `task show ID=...` | `infer conversations list` / `infer conversations show`, the recorded fires |
| `task all` | Runs the four headless scenarios above |
| `task clean` | Removes the demo HOME |

## What to expect

The headless tasks filter their JSON output down to the `role=tool` entry. For `task create:quote` its
message (line breaks added, the real one is a single JSON string):

```text
{"tool_name":"Schedule", ... "success":true,
   "message":"Scheduled recurring job 05d34925-... created; output is recorded to storage (no delivery
   channel). Fires while 'infer daemon' is running."}
```

`task create:reminder` - same shape, the message starts with `Scheduled one-off job <id> created`.

`task list` - the result carries every job with its ID, cron expression, prompt and `last_run` stamp, which
is where the ID for `update`/`delete` comes from:

```text
{"tool_name":"Schedule", ... "operation":"list", "message":"3 scheduled job(s).",
   "jobs":[{"id":"05d34925-...","name":"Reading quote","cron_expression":"@every 1m",...}]}
```

`task jobs` after a fire - prints every job file with its path, plus any run records in `runs/`. The
recurring job's file now also carries `last_run`, written back by the daemon after every fire.

`task runs` - lists one conversation per fire (the fire's session ID doubles as its conversation ID) plus
any of your own runs. `task show ID=<id>` prints the fire's prompt and the mock model's reply.

`task delete` - the result message is `Deleted job demo-standup-summary.`, the daemon logs
`scheduled job removed` within two seconds and the file `.home/.infer/schedules/demo-standup-summary.yaml`
is gone.

## Approval

The headless tasks run with `--mode auto`, where tool calls execute without approval - headless mode has no
one to ask, and the demo would otherwise only print `Blocked: Schedule requires approval`. In a real chat
the default `tools.schedule.require_approval: true` is worth keeping; the user confirms every create,
update or delete before it runs (see the
[security notes](../../docs/scheduling.md#security-considerations)).

## Without Task

The Taskfile only sets environment variables and a `HOME`. To run one prompt yourself from this directory:

```bash
export INFER_GATEWAY_MOCK=true
export INFER_GATEWAY_MOCK_SCENARIOS="$PWD/scenarios.yaml"
export INFER_AGENT_MODEL=mock/openai/gpt-4o
export INFER_TOOLS_SCHEDULE_ENABLED=true
export HOME="$PWD/.home"

../../infer headless --mode auto "schedule an inspiring quote every minute for me" | grep '"role":"tool"'
HOME="$PWD/.home" ../../infer daemon    # fires while this runs; Ctrl+C when done
HOME="$PWD/.home" ../../infer conversations list
```

## Use a real model

Drop `INFER_GATEWAY_MOCK`, `INFER_GATEWAY_MOCK_SCENARIOS` and `INFER_AGENT_MODEL` and the same flow runs
against a real model: it decides what to schedule, the daemon fires the prompt verbatim. Cron syntax, the
`get` and `update` operations and delivery through channels (Telegram and friends) are in the
[Scheduling Guide](../../docs/scheduling.md).
