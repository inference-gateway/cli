# Conversation Storage

The CLI supports configurable conversation storage, allowing you to save, resume, and manage your chat
sessions across different invocations. By default, conversations are stored in memory, but you can enable
persistent storage using SQLite, PostgreSQL, or Redis.

## Overview

The conversation storage system provides:

- **Configurable Storage**: Choose between in-memory (default), SQLite, PostgreSQL, or Redis
- **Conversation Management**: List, save, load, and delete conversations using `/conversations`
- **Unified Interface**: Consistent API across all storage backends

## Storage Backends

### SQLite (Recommended for local use)

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
  enabled: false  # Set to true to enable persistent storage
  type: memory    # Options: memory (default), sqlite, postgres, or redis

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

1. **For in-memory storage (default)**:
   - No configuration needed, or set `enabled: false`
   - Conversations are lost when the CLI exits

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
## Recent Conversations

**1.** Planning the Q4 Product Roadmap
   📅 Jan 15, 2024 2:30 PM • 💬 12 messages • 🔤 1,245 tokens
   🤖 claude-3 • 🏷️ planning, roadmap
   📋 ID: `a1b2c3d4-e5f6-7890-abcd-ef1234567890`

**2.** Discussion about API Design
   📅 Jan 14, 2024 4:15 PM • 💬 8 messages • 🔤 892 tokens
   🤖 claude-3 • 🏷️ api, design
   📋 ID: `b2c3d4e5-f6g7-8901-bcde-f23456789012`
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
