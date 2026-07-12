# PostgreSQL conversation storage

Persist conversations to PostgreSQL instead of the default local JSONL files.
Useful when several `infer` processes — chat, the `channels-manager` daemon, the
web terminal — should share one conversation store.

`docker-compose.yml` runs the Inference Gateway plus a PostgreSQL container;
`.infer/config.yaml` points the CLI at both. The CLI creates its schema
automatically on first connect (via `infer migrate`, run implicitly), so there
is nothing to set up in the database by hand.

## Run it

```bash
# 1. Provider keys for the gateway
cp .env.example .env   # then edit — set at least one provider key

# 2. Start the gateway + PostgreSQL
docker compose up -d

# 3. Chat — conversations are written to PostgreSQL
infer config set agent.model anthropic/claude-4.5-sonnet
infer chat
```

(Install the CLI first if needed: `flox activate -- task install`, or the
install script from the repo root.)

## Verify persistence

```bash
# From the CLI — lists conversations read back out of PostgreSQL
infer conversations list

# Straight from the database
docker compose exec postgres \
  psql -U infer -d infer_conversations -c \
  'SELECT id, title, count, updated_at FROM conversations ORDER BY updated_at DESC;'
```

Conversations survive `docker compose down && docker compose up -d` because the
data lives in the `postgres_data` volume. `docker compose down -v` wipes it.

## Configuration

The relevant block in `.infer/config.yaml`:

```yaml
storage:
  enabled: true
  type: postgres
  postgres:
    host: localhost
    port: 5432
    database: infer_conversations
    username: infer
    password: infer
    ssl_mode: disable   # local container has no TLS; use "require" for managed PG
```

Any field can be overridden by an environment variable, which is the safer place
for a real password:

```bash
export INFER_STORAGE_TYPE=postgres
export INFER_STORAGE_POSTGRES_PASSWORD='…'
```

SQLite and Cloudflare D1 use the same single-table schema and are drop-in
alternatives — set `storage.type` to `sqlite` or `d1` (see
[../../docs/conversation-storage.md](../../docs/conversation-storage.md)).
