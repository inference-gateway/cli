# channels

**What** - the channels capability: the contract a remote messaging channel implements so the agent can be driven from it.
**Why** - the daemon and the scheduler must reach a user on a messaging platform without depending on that platform's SDK.
**How** - `contracts.go` defines `Channel` plus the optional `ApprovalChannel` and `HistoryCleaner`, and adapters
implement them where their SDK lives.

## How it plugs in

- Telegram is the only adapter today, in `internal/presentation/telegram`.
- `infer daemon` runs the configured channels, and the scheduler's notifier delivers job results through the same contract.

## Related

- [Channels](../../docs/channels.md)
- [Scheduling](../../docs/scheduling.md)
- [Heartbeat](../../docs/heartbeat.md)
