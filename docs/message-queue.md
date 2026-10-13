# Message Queue

The message queue holds messages waiting for the agent to process on its next
availability - text typed while a turn is running, background-job completion
notes, and work pushed by other processes. The backend that backs the queue is
configurable: `memory` (the default, the status quo before queues became
durable), `sqlite`, `postgres` or `redis`.

## Backends

| Backend | Survives a restart | Shareable across processes | Connection settings |
| ------- | ------------------ | -------------------------- | ------------------- |
| `memory` (default) | no | no | - |
| `sqlite` | yes | yes | `storage.sqlite` |
| `postgres` | yes | yes | `storage.postgres` |
| `redis` | yes | yes | `storage.redis` |

`queue.type` picks the backend. It is deliberately separate from
`storage.type`: the storage default `jsonl` is not a queue backend, and
following `storage.type` would silently make the default queue durable.
Non-memory backends reuse the matching `storage.<type>` connection settings, so
a queue never grows a second host/port/password block. An unreachable backend
fails startup with a clear error naming the backend - it never silently falls
back to memory. `jsonl` and `d1` are rejected by config validation with a clear
error.

```yaml
queue:
  type: redis        # memory (default), sqlite, postgres or redis
  name: ""           # queue scope, see Scoping below
  poll_interval: 2s  # cross-process look cadence, durable backends only
```

Environment overrides follow the usual scheme:
`INFER_QUEUE_TYPE`, `INFER_QUEUE_NAME`, `INFER_QUEUE_POLL_INTERVAL`. See the
[Configuration Reference](configuration-reference.md#queue-configuration).

## Semantics

- FIFO per scope, oldest message first.
- A drain takes every message out in one atomic step - `DELETE ... RETURNING`
  (PostgreSQL with `FOR UPDATE SKIP LOCKED`, SQLite in one statement) or one
  server-side Lua step in Redis. Two processes draining at the same time never
  receive the same message.
- Delivery is at-most-once, as with the process-local queue: a drained message
  lost to a crash before it reaches the conversation is gone. Ack and
  redelivery are out of scope.
- The memory queue keeps today's behaviour exactly: process-local, nothing
  survives a restart.

## Scoping

`queue.name` scopes the queue. It is resolved once at startup and never changes
while infer runs. Empty means this process's session ID, so two local sessions
never take each other's typed messages - but a new session is a new scope, so
nothing carries over a restart. Set an explicit `queue.name` for anything that
should outlive the process or be fed from outside: the same name on every
process that should consume the work, and on every external producer.

## Cross-process wake-up

A poller on each durable backend reads the queue every `queue.poll_interval`
(default `2s`) and wakes an idle agent when another producer pushed entries,
so a message written from outside is picked up within that interval without
any of the producers talking to infer directly. The status-bar queue reads are
answered from a local snapshot refreshed by writes and polls, so the render
path never blocks on the backend.

## Status-bar indicator

`status_bar.indicators.queue` gates the indicator. The memory queue hides it
while empty and shows the icon plus `N queued` during work, exactly as before.
A durable backend is always visible and names the backend - `redis` when
empty, `redis 3 queued` when work waits - and it renders in the error color
while the backend is unreachable.

## Feeding the queue from outside infer

External producers enqueue by writing the record to the configured backend.
The record is one JSON object with these fields:

| Field | Type | Notes |
| ----- | ---- | ----- |
| `role` | string | SDK role of the message, `"user"` for user work |
| `content` | JSON | the SDK message content - a plain text message is a JSON string |
| `source` | string | one of `composer`, `stdin`, `a2a`, `shell`, `subagent`, `job`; use `a2a` for work another agent pushes |
| `request_id` | string | free-form producer-supplied id for tracing and ordering, empty allowed |
| `queued_at` | RFC3339 timestamp | when the producer created the entry |

```json
{
  "role": "user",
  "content": "run the nightly regression checks",
  "source": "a2a",
  "request_id": "nightly-2026-10-12",
  "queued_at": "2026-10-12T20:00:00Z"
}
```

### Redis

One list per queue scope, oldest entry at the head. Key:
`infer:queue:<name>`.

```bash
redis-cli RPUSH infer:queue:my-shared-queue '{
  "role": "user",
  "content": "run the nightly regression checks",
  "source": "a2a",
  "request_id": "nightly-2026-10-12",
  "queued_at": "2026-10-12T20:00:00Z"
}'
```

- `RPUSH` appends to the tail; infer consumes from the head, so producers
  never prepend.
- `LRANGE infer:queue:<name> 0 -1` reads the queue without consuming.
- The consumer empties the key in a single Lua step, so readers never race a
  drain into receiving a message twice.

### SQLite and PostgreSQL

Both dialects share the `message_queue` table (migration 009):

| Column | Notes |
| ------ | ----- |
| `id` | sequence key that defines the FIFO order - do not set it by hand |
| `queue_name` | the queue scope |
| `role` | SDK role of the message |
| `content` | the SDK message content as JSON |
| `source` | one of `composer`, `stdin`, `a2a`, `shell`, `subagent`, `job` |
| `request_id` | producer-supplied id, empty allowed |
| `queued_at` | `DATETIME` (SQLite) or `TIMESTAMP WITH TIME ZONE` (PostgreSQL) |

```sql
INSERT INTO message_queue (queue_name, role, content, source, request_id, queued_at)
VALUES (
  'my-shared-queue',
  'user',
  '"run the nightly regression checks"',
  'a2a',
  'nightly-2026-10-12',
  NOW()
);
```

## See Also

- [Example: message queue backends](../examples/queue-backends/README.md) - a runnable setup for each backend
- [Configuration Reference](configuration-reference.md)
- [Conversation Storage](conversation-storage.md)
- [Database Migrations](database-migrations.md)
