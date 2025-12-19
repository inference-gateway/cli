# Conversation Storage

The CLI supports configurable conversation storage, allowing you to save, resume, and manage your chat
sessions across different invocations. By default, conversations are stored using JSONL (JSON Lines) files,
which provides zero-dependency persistent storage. You can also choose SQLite, PostgreSQL, or Redis.

## Overview

The conversation storage system provides:

- **Configurable Storage**: Choose between JSONL (default), SQLite, PostgreSQL, Redis, or in-memory
- **Conversation Management**: List, save, load, and delete conversations using `/conversations`
- **Unified Interface**: Consistent API across all storage backends

## Storage Backends

### JSONL (Default - Recommended for personal use)

JSONL (JSON Lines) provides a simple, file-based storage solution perfect for personal use with zero dependencies.

**Configuration:**

```yaml
storage:
  enabled: true
  type: jsonl
  jsonl:
    path: ~/.infer/conversations
```

**Pros:**

- No external dependencies (no database, no CGO)
- Human-readable format (text files)
- Easy to backup, sync, and version control
- Git-friendly (text-based)
- Zero setup required
- Works on all platforms
- Fast for typical usage (dozens to hundreds of conversations)

**Cons:**

- Not suitable for thousands of conversations
- No advanced querying capabilities
- Sequential file access (vs indexed database)

**File Structure:**

Each conversation is stored in a separate JSONL file in the configured directory:

```text
~/.infer/conversations/
├── <conversation-id-1>.jsonl
├── <conversation-id-2>.jsonl
└── <conversation-id-3>.jsonl
```

Each file contains exactly two lines:

- **Line 1**: Metadata (JSON object)
- **Line 2**: Entries array (JSON array)

**Backup:**

Simply copy the conversations directory:

```bash
cp -r ~/.infer/conversations ~/backups/conversations-$(date +%Y%m%d)
```

**Version Control:**

Works well with Git:

```bash
cd ~/.infer/conversations
git init
git add *.jsonl
git commit -m "Save conversations"
```

### SQLite (Alternative for local use)

SQLite provides a lightweight, file-based storage solution perfect for personal use:

```yaml
storage:
  type: sqlite
  sqlite:
    path: ~/.infer/conversations.db
```

**Pros:**

- No external dependencies
- Fast local access
- Automatic schema management
- Perfect for single-user scenarios

**Cons:**

- Not suitable for multi-user environments
- Limited concurrent access

### PostgreSQL (Recommended for teams)

PostgreSQL offers enterprise-grade features for team environments:

```yaml
storage:
  type: postgres
  postgres:
    host: localhost
    port: 5432
    database: infer_conversations
    username: infer_user
    password: your_password
    ssl_mode: require
```

**Pros:**

- Multi-user support
- ACID compliance
- Advanced indexing and search
- JSON/JSONB support for metadata

**Cons:**

- Requires PostgreSQL server
- More complex setup

### Redis (Recommended for temporary storage)

Redis provides fast, in-memory storage with optional persistence:

```yaml
storage:
  type: redis
  redis:
    host: localhost
    port: 6379
    database: 0
    password: ""  # optional
    username: ""  # optional
    ttl: 2592000  # 30 days in seconds, 0 for no expiration
```

**Pros:**

- Extremely fast access
- Built-in expiration (TTL)
- Scalable clustering support
- Great for temporary conversations

**Cons:**

- Requires Redis server
- Memory-based (can be expensive for large datasets)
- Data loss risk if not properly configured for persistence

## Configuration

Add storage configuration to your `.infer/config.yaml`:

```yaml
# Storage configuration
storage:
  enabled: true       # true to enable persistent storage
  type: jsonl         # Options: jsonl (default), sqlite, postgres, redis, memory

  # JSONL configuration (used when type: jsonl)
  jsonl:
    path: ~/.infer/conversations  # Directory for JSONL files

  # SQLite configuration (used when type: sqlite)
  sqlite:
    path: conversations.db  # Relative to .infer directory or absolute path

  # PostgreSQL configuration (used when type: postgres)
  postgres:
    host: localhost
    port: 5432
    database: infer_conversations
    username: "%POSTGRES_USER%"     # Can use environment variables
    password: "%POSTGRES_PASSWORD%" # Can use environment variables
    ssl_mode: prefer

  # Redis configuration (used when type: redis)
  redis:
    host: localhost
    port: 6379
    password: "%REDIS_PASSWORD%"  # Can use environment variables
    db: 0  # Redis database number
```

### Enabling Storage

1. **For JSONL storage (default)**:

   ```yaml
   storage:
     enabled: true
     type: jsonl
     jsonl:
       path: ~/.infer/conversations
   ```

2. **For SQLite storage**:

   ```yaml
   storage:
     enabled: true
     type: sqlite
     sqlite:
       path: conversations.db
   ```

3. **For PostgreSQL storage**:

   ```yaml
   storage:
     enabled: true
     type: postgres
     postgres:
       host: your-postgres-host
       port: 5432
       database: infer_conversations
       username: "%POSTGRES_USER%"
       password: "%POSTGRES_PASSWORD%"
   ```

4. **For Redis storage**:

   ```yaml
   storage:
     enabled: true
     type: redis
     redis:
       host: your-redis-host
       port: 6379
       password: "%REDIS_PASSWORD%"
   ```

5. **For in-memory storage**:
   - Set `enabled: false` or `type: memory`
   - Conversations are lost when the CLI exits

## Usage

### Starting a New Conversation

When you start the CLI, you automatically begin a new conversation. To explicitly start with a title:

```bash
/save My Important Discussion
```

### Saving Conversations

Save your current conversation:

```bash
# Save with auto-generated title
/save

# Save with custom title
/save Discussion about API Design

# Save with multi-word title
/save Planning the Q4 Product Roadmap
```

### Resuming Conversations

List recent conversations:

```bash
/conversations
```

This shows:

```text
Select a Conversation

Press / to search • 4 conversations available

ID                     │ Summary                                  │ Updated              │ Messages
─────────────────────────────────────────────────────────────────────────────────────────────────────
▶ fdd90f83-0b84-486... │ Implementing Redis cache layer           │ 2025-08-27 00:55:29  │ 2
  22de96f6-577d-4df... │ Debugging API authentication flow        │ 2025-08-27 00:32:25  │ 12
  b199fae0-b0cd-418... │ Setting up PostgreSQL migrations         │ 2025-08-27 00:27:20  │ 6
  ca79a501-ef90-4e0... │ Refactoring conversation storage         │ 2025-08-26 23:52:59  │ 4

─────────────────────────────────────────────────────────────────────────────────────────────────────
```

Resume by number or ID:

```bash
# Use /conversations to select and load a conversation interactively
/conversations
```

### Managing Conversations

Delete a conversation:

```bash
# Use /conversations to select a conversation and press 'd' to delete it
/conversations
```

## Data Structure

### Conversation Metadata

Each conversation includes:

- **ID**: Unique identifier (UUID)
- **Title**: Human-readable title
- **Created/Updated**: Timestamps
- **Message Count**: Number of messages
- **Token Statistics**: Usage tracking
- **Model**: AI model used
- **Tags**: Organizational labels
- **Summary**: Optional conversation summary

### Message Storage

Messages are stored with:

- **Content**: Message text
- **Role**: user, assistant, system, or tool
- **Timestamp**: When the message was created
- **Model**: AI model used for this message
- **Tool Execution**: Results of tool calls
- **System Reminder Flag**: Internal system messages

## Database Schema

### SQLite/PostgreSQL

```sql
-- Conversations table
CREATE TABLE conversations (
    id VARCHAR(255) PRIMARY KEY,
    title TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    message_count INTEGER NOT NULL DEFAULT 0,
    model VARCHAR(255),
    tags JSON,
    summary TEXT,
    token_stats JSON
);

-- Conversation entries table
CREATE TABLE conversation_entries (
    id BIGSERIAL PRIMARY KEY,
    conversation_id VARCHAR(255) NOT NULL,
    entry_data JSON NOT NULL,
    sequence_number INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
);
```

### Redis

```text
# Conversation metadata
conversation:{id} -> JSON metadata

# Conversation entries
conversation:{id}:entries -> JSON array of entries

# Conversation index (sorted by update time)
conversations:index -> sorted set (score: timestamp, member: conversation_id)
```

## Best Practices

### Performance

1. **SQLite**: Keep database file on fast storage (SSD)
2. **PostgreSQL**: Use connection pooling for high-concurrency scenarios
3. **Redis**: Configure appropriate memory policies and persistence

### Security

1. **Database Credentials**: Use environment variables or secure credential storage
2. **Network Security**: Use SSL/TLS for network connections
3. **Access Control**: Implement proper user authentication and authorization

### Backup

1. **SQLite**: Regular file system backups of the `.db` file
2. **PostgreSQL**: Use `pg_dump` for regular backups
3. **Redis**: Configure RDB or AOF persistence

### Monitoring

1. **Health Checks**: The storage interface includes health check methods
2. **Error Handling**: Failed operations are logged and don't interrupt the session
3. **Auto-save**: Conversations are automatically saved after each interaction

## Troubleshooting

### Common Issues

#### SQLite Permission Errors

```bash
# Ensure directory exists and is writable
mkdir -p ~/.infer
chmod 755 ~/.infer
```

#### PostgreSQL Connection Issues

```yaml
# Check connection parameters
storage:
  type: postgres
  postgres:
    host: localhost  # Verify host
    port: 5432       # Verify port
    ssl_mode: disable  # Try without SSL first
```

#### Redis Connection Issues

```yaml
# Verify Redis is running
redis:
  host: localhost
  port: 6379
  database: 0
  password: ""  # Remove if no auth
```

### Migration

When switching storage backends, you'll need to export/import conversations manually. The CLI provides export
functionality that can help with migration:

```bash
/compact  # Exports current conversation to markdown
```

## API Reference

### Storage Interface

```go
type ConversationStorage interface {
    SaveConversation(ctx context.Context, conversationID string,
        entries []domain.ConversationEntry, metadata ConversationMetadata) error
    LoadConversation(ctx context.Context, conversationID string) (
        []domain.ConversationEntry, ConversationMetadata, error)
    ListConversations(ctx context.Context, limit, offset int) ([]ConversationSummary, error)
    DeleteConversation(ctx context.Context, conversationID string) error
    UpdateConversationMetadata(ctx context.Context, conversationID string,
        metadata ConversationMetadata) error
    Close() error
    Health(ctx context.Context) error
}
```

### Factory Function

```go
// Create storage instance from configuration
storage, err := storage.NewStorage(config)
if err != nil {
    log.Fatal("Failed to create storage:", err)
}
defer storage.Close()
```
