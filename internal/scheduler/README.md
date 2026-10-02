# scheduler

**What** - the scheduler bounded context: cron-driven jobs, the heartbeat, and the registry of background work (shells, subagents, supervisor jobs).
**Why** - scheduled work runs inside `infer daemon`, outside any chat session, so it needs its own lifecycle and storage
instead of living in the agent loop.
**How** - the `Schedule` tool writes jobs to storage, and the daemon's scheduler polls that storage and runs each due
job as a one-shot `infer headless` inside `infer daemon`, through `internal/agent/headless`. Heartbeat wake-ups
run the same way. Each is one process per run, no client follows it, and its stderr lands in the daemon log.

## How it plugs in

- Jobs persist through the configured storage (`~/.infer/schedules/*.yaml` with jsonl), or through GitHub
  Actions with `scheduler.backend: github` (`githubscheduler/`).
- The scheduler polls every few seconds, so new jobs fire without a restart. `schedule_notifier.go` delivers
  results back through the originating channel.
- `heartbeat/` wakes the agent periodically to check pending work. `jobs/` supervises long-running background jobs.
- Title backfill runs under `infer conversation-title daemon`.
- Channels do not use `agentheadless`: they drive the daemon's session workers through the `sessions` registry.

## Related

- [infer daemon](../../docs/daemon.md)
- [Scheduling](../../docs/scheduling.md)
- [Heartbeat](../../docs/heartbeat.md)
- [Tools Reference](../../docs/tools-reference.md#schedule-tool)
