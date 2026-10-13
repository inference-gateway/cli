# Message queue backends

`queue.type` picks the backend that holds messages waiting for the agent. The
default `memory` queue is process-local and holds nothing across a restart.
The `sqlite`, `postgres` and `redis` backends are durable: queued work
survives a restart and any external process can feed the queue by writing one
record. This one example covers all four backends - they differ only in
configuration.

| Backend | Needs | Reuses connection settings from |
| ------- | ----- | ------------------------------- |
| `memory` (default) | nothing | - |
| `sqlite` | nothing | `storage.sqlite` |
| `postgres` | a PostgreSQL server | `storage.postgres` |
| `redis` | a Redis server | `storage.redis` |

A durable backend dials with the matching `storage.<type>` block, so the queue
never grows a second host or password config. `docker-compose.yml` runs the
gateway plus both server backends for the last two.

## Run it

```bash
# 1. Provider keys for the gateway
touch .env   # then add at least one provider key - see examples/basic

# 2. Start the server your backend needs (skip for memory and sqlite)
docker compose up -d

# 3. Chat - the status bar names the backend while it is durable
infer chat
```

(Install the CLI first if needed: `flox activate -- task install`, or the
install script from the repo root.)

Add to `~/.infer/config.yaml` - here for `redis`, the same shape works for
`postgres` with its own storage block (see
[../postgres-storage](../postgres-storage/README.md)), and `sqlite` needs only
`queue.type: sqlite` plus optionally `storage.sqlite.path`:

```yaml
queue:
  type: redis        # memory (default), sqlite, postgres or redis
  name: infer-shared # one queue shared by every infer that sets the name
storage:
  redis:
    host: localhost
    port: 6379
```

`name` scopes the queue. Without it the scope is the process session id, which
isolates one process but changes on every start - with an explicit name, the
queue survives restarts and several infer processes and external producers
share exactly this queue.

## Feed work from outside

A producer writes one JSON record with `role`, `content`, `source`,
`request_id` and `queued_at`; the field reference is
[../../docs/message-queue.md](../../docs/message-queue.md#feeding-the-queue-from-outside-infer).
One command per backend, with `queue.name: infer-shared` as above. The SQL
table is created by infer's schema migration the first time it opens that
backend, so start `infer chat` once with the queue configured before the
`postgres` or `sqlite` insert:

```bash
# redis - one list per queue scope, consume from the head
docker compose exec redis redis-cli RPUSH infer:queue:infer-shared '{
  "role": "user",
  "content": "run the nightly regression checks",
  "source": "a2a",
  "request_id": "nightly-2026-10-12",
  "queued_at": "2026-10-12T20:00:00Z"
}'

# postgres - the message_queue table schema migration 009 creates
docker compose exec postgres psql -U infer -d infer -c "
  INSERT INTO message_queue (queue_name, role, content, source, request_id, queued_at)
  VALUES ('infer-shared', 'user', '\"run the nightly regression checks\"',
          'a2a', 'nightly-2026-10-12', NOW())"

# sqlite - the same table in the queue's database file
sqlite3 ~/.infer/conversations.db "
  INSERT INTO message_queue (queue_name, role, content, source, request_id, queued_at)
  VALUES ('infer-shared', 'user', '\"run the nightly regression checks\"',
          'a2a', 'nightly-2026-10-12', datetime('now'))"
```

`source` marks where the work came from - `composer` for messages typed while
the agent is busy, `stdin` from piped runs, `a2a` for work another process
pushes, and `shell`, `subagent` and `job` for finished background work. infer
already routes background-job results through the same queue itself: a finished
background shell lands as a `[Shell Completed: ...]` note and the agent reads it
on its next turn.

## Verify durability

1. With `infer` not running, put the record above on the queue.
2. `infer chat` - the status bar immediately shows the backend and the count,
   `redis 1 queued`.
3. Within `queue.poll_interval` the poller wakes the idle agent and it
   processes the queued message without anyone typing. The indicator drops back
   to the bare backend name once the queue is empty.

## See Also

- [Message Queue](../../docs/message-queue.md) - backends, semantics, record format
- [PostgreSQL conversation storage](../postgres-storage/README.md) - the same pattern for conversations
