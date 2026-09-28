# scheduler

**What** - the scheduler bounded context: cron-driven jobs, the heartbeat, and the registry of background work (shells, subagents, supervisor jobs).
**Why** - scheduled work runs inside `infer daemon`, outside any chat session, so it needs its own lifecycle and storage
instead of living in the agent loop.
**How** - the `Schedule` tool writes jobs to storage, and the daemon's scheduler polls that storage and runs each due
job in a fresh `infer headless` session.

## How it plugs in

- Jobs persist through the configured storage (`~/.infer/schedules/*.yaml` with jsonl), or through GitHub
  Actions with `scheduler.backend: github` (`githubscheduler/`).
- The scheduler polls every few seconds, so new jobs fire without a restart. `schedule_notifier.go` delivers
  results back through the originating channel.
- `heartbeat/` wakes the agent periodically to check pending work. `jobs/` supervises long-running background jobs.
- Title backfill runs under `infer conversation-title daemon`.

## Related

- [Scheduling](../../docs/scheduling.md)
- [Heartbeat](../../docs/heartbeat.md)
- [Tools Reference](../../docs/tools-reference.md#schedule-tool)
