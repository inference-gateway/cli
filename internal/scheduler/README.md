# scheduler

**What** - the scheduler bounded context: cron-driven jobs, one-off reminders, background shell tracking and the daemon-side notifier.
**Why** - scheduled work runs inside `infer daemon`, outside any chat session, so it needs its own
lifecycle and storage instead of living in the agent loop.
**How** - the Schedule tool persists jobs through the conversation storage backend, and `infer daemon` hosts this service to fire them.

## What it owns

- `cron.go` - the scheduler core: polling storage, deciding what fires, and running each job.
- `schedule_notifier.go` - deriving the delivery channel and recipient from the originating session.
- `background_shell_service.go` and `background_task_registry.go` - tracking long-running shell processes started by the agent.
- `title_backfill.go` - driving conversation titles in daemon mode.

## Why it is separate

A job fires in a brand-new `infer headless` session with no carried context, and only while the
daemon runs. That execution model is different enough from an interactive turn to deserve its own
package and its own failure handling.

## How it plugs in

- The `Schedule` tool (owned by the tools context) calls into this service; the tool lives behind `tools.schedule.enabled`.
- `infer daemon` hosts the scheduler alongside channels and heartbeat, and the scheduler polls storage every few seconds so new jobs fire without a restart.
- Jobs are persisted as YAML under `~/.infer/schedules/` with the default jsonl backend.

## Related

- [Scheduling](../../docs/scheduling.md)
- [Channels](../../docs/channels.md)
- [Tools Reference](../../docs/tools-reference.md#schedule-tool)
