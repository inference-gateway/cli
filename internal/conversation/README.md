# conversation

**What** - the conversation bounded context: sessions, message history, token accounting, pricing and title generation.
**Why** - a chat is state that outlives one turn and can live in five different storage backends, so it
is a domain of its own rather than a field on the agent.
**How** - the agent talks to `domain/` contracts, and `conversation.go` plus `persistent_conversation.go` implement them over the configured storage backend.

## What it owns

- `conversation.go`, `model.go` - the in-memory conversation and its value types.
- `persistent_conversation.go` - reading and writing through a storage backend.
- `message_queue.go` - queued messages drained between turns.
- `session_rollover.go` - starting a fresh session while keeping the thread.
- `conversation_optimizer.go` - compaction and context-window management.
- `conversation_title_generator.go` - AI-powered titles, including the background backfill.
- `pricing_service.go`, `tokenizer.go` - cost and token estimation.
- `event_bridge.go` - translating conversation events for the UI.
- `domain/` - the contracts: conversation, agent messages, contracts, pricing, queue, session, token estimator.

## Why it is separate

Storage backends (jsonl, SQLite, Postgres, Redis, D1) and the migration system change for their own
reasons. Keeping them behind `domain/` means the agent never learns which one is configured.

## How it plugs in

- `internal/container/container.go` builds the service and the storage factory from `storage.*` config.
- `infer conversations`, `infer export`, `infer conversation-title` and the `/conversations` shortcut are thin adapters over this context.

## Related

- [Conversation Storage](../../docs/conversation-storage.md)
- [Conversation Versioning](../../docs/conversation-versioning.md)
- [Conversation Title Generation](../../docs/conversation-title-generation.md)
- [Database Migrations](../../docs/database-migrations.md)
