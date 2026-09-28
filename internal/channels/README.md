# channels

**What** - the channels bounded context: the contracts that a remote messaging channel (Telegram, WhatsApp, or a custom one) must satisfy.
**Why** - the agent must be reachable from messaging platforms without depending on any platform SDK, so this context owns only the ports.
**How** - adapters live where the SDK belongs (`internal/presentation/telegram` for go-telegram), implement these contracts, and the daemon drives them.

## What it owns

- `contracts.go` - the channel interface: receiving inbound messages, sending replies, and the per-channel configuration the runtime needs.
- The rules for per-sender sessions, image retention, and the approval round-trip that a channel implementation must honour.

## Why it is separate

go-telegram and the Meta Business API are presentation-layer dependencies. Depguard forbids them
outside `presentation/`, so the contracts that the agent and the daemon consume live here instead, as
a small, implementation-free package.

## How it plugs in

- Enable channels in `.infer/channels.yaml` (seeded by `infer init`) or with `INFER_CHANNELS_*` variables, then run `infer daemon`.
- Each inbound message triggers `infer headless --session-id <id>` with a persistent session per sender.
- Access is allowlist-only and empty allowlists reject everything; tool approvals are forwarded to the channel by default.

## Related

- [Channels](../../docs/channels.md)
- [Scheduling](../../docs/scheduling.md)
- [Heartbeat](../../docs/heartbeat.md)
