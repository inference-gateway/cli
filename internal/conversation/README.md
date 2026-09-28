# conversation

**What** - the conversation bounded context: sessions, message history, the message queue, compaction, pricing, token estimation and title generation.
**Why** - a chat is state that outlives one turn and can sit in any of several storage backends, so it is a domain of
its own rather than a field on the agent.
**How** - the agent talks to the `domain/` contracts, and `persistent_conversation.go` implements them over the configured storage backend.

## How it plugs in

- The storage backends (jsonl, SQLite, PostgreSQL, Redis, Cloudflare D1, and in-memory when storage is off) live
  in `internal/platform/storage`. The agent never learns which one is configured.
- `internal/container/container.go` builds the service and the storage from `storage.*` config.
- `infer conversations`, `infer export`, `infer conversation-title` and the `/conversations` shortcut are thin adapters over this context.

## Related

- [Conversation Storage](../../docs/conversation-storage.md)
- [Conversation Versioning](../../docs/conversation-versioning.md)
- [Conversation Title Generation](../../docs/conversation-title-generation.md)
- [Database Migrations](../../docs/database-migrations.md)
