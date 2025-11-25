# Conversation Title Generation

The CLI includes an AI-powered conversation title generation system that automatically creates meaningful titles
for your conversations. This helps you organize and find conversations more easily.

## Overview

The title generation system provides:

- **Automatic AI-generated titles**: Generate concise, descriptive titles based on conversation content
- **Background processing**: Titles are generated automatically without blocking the user interface
- **Smart invalidation**: Titles are regenerated when conversations are resumed or modified
- **Configurable models**: Choose which AI model to use for title generation
- **User opt-out**: Disable title generation with fallback to first 10 words
- **Individual processing**: Each conversation is processed separately for better reliability

## Configuration

Configure title generation in your `.infer/config.yaml`:

```yaml
conversation:
  title_generation:
    enabled: true                    # Enable/disable title generation
    model: "anthropic/claude-4-haiku" # AI model for title generation
    batch_size: 10                   # Number of conversations to process per batch
    interval: 300                    # Background job interval in seconds (default: 300 = 5 minutes)
    system_prompt: |                 # Custom system prompt (optional)
      Generate a concise conversation title based on the messages provided.

      REQUIREMENTS:
      - MUST be under 50 characters total
      - MUST be descriptive and capture the main topic
      - MUST use title case
      - NO quotes, colons, or special characters
      - Focus on the primary subject or task discussed

      EXAMPLES:
      - "React Component Testing"
      - "Database Migration Setup"
      - "API Error Handling"
      - "Docker Configuration"

      Respond with ONLY the title, no quotes or explanation.
```

### Configuration Options

- **enabled**: Enable or disable automatic title generation (default: true)
- **model**: AI model to use for title generation. Falls back to `agent.model` if not specified
- **batch_size**: Number of conversations to process in each background job run (default: 10)
- **interval**: Background job interval in seconds (default: 300 = 5 minutes)
- **system_prompt**: Custom prompt for title generation. Uses default if not specified

## How It Works

### Automatic Title Generation

1. **New Conversations**: When a new conversation is created, it starts with a basic title derived from the first user message
2. **Background Processing**: A background service periodically scans for conversations that need
   AI-generated titles
3. **AI Generation**: The system sends the conversation content to the configured AI model with instructions
   to create a concise title
4. **Title Updates**: Generated titles are saved to the conversation metadata

### Title Invalidation

Titles are automatically invalidated and regenerated when:

- A conversation is resumed after being saved
- New messages are added to an existing conversation with a generated title
- The conversation content changes significantly

### Fallback Mechanism

If title generation is disabled or fails, the system falls back to:

- Taking the first 10 words from the first user message
- Truncating to 50 characters maximum
- Using "Conversation" if no suitable content is found

## Commands

### Generate Titles Manually

Trigger title generation for all pending conversations:

```bash
infer conversation-title generate
```

This command will:

- Find all conversations that need titles (new or invalidated)
- Process them individually using the configured AI model
- Update the conversation metadata with generated titles
- Report progress and completion time

### Check Title Generation Status

View the current status of title generation:

```bash
infer conversation-title status
```

This shows:

- Current configuration settings
- Whether background jobs are running
- Number of conversations pending title generation
- List of pending conversations (up to 10)

## Technical Details

### Database Schema

The title generation system adds the following fields to conversation metadata:

- `title_generated`: Boolean flag indicating if the title was AI-generated
- `title_invalidated`: Boolean flag indicating if the title needs regeneration
- `title_generation_time`: Timestamp of when the title was generated

### Background Jobs

The background job system:

- Runs at configurable intervals when persistent storage is enabled (default: 5 minutes)
- Processes conversations in batches according to `batch_size` setting
- Handles each conversation individually to prevent batch failures
- Includes error handling and logging for failed title generations

### Performance Considerations

- Title generation uses individual API calls for each conversation (no batching)
- Background processing prevents blocking the user interface
- Generated titles are cached to avoid repeated API calls
- The system respects model token limits and conversation size constraints

## Storage Backend Support

Title generation works with all supported storage backends:

- **SQLite**: Full support with database schema migration
- **PostgreSQL**: Full support with database schema migration
- **Redis**: Full support using JSON metadata storage
- **In-Memory**: Not supported (titles would be lost on restart)

## Best Practices

1. **Model Selection**: Use fast, efficient models like `claude-4-haiku` for title generation to minimize latency
2. **Batch Size**: Keep batch sizes reasonable (5-20) to balance performance and resource usage
3. **Monitoring**: Check the status command periodically to ensure titles are being generated
4. **Fallback**: Even with title generation disabled, the first-10-words fallback provides reasonable titles

## Troubleshooting

### Common Issues

#### Title Generation Not Working

```bash
# Check if storage is enabled
infer conversation-title status

# Ensure you have persistent storage configured
# Title generation requires storage.enabled: true in config
```

#### Slow Title Generation

- Reduce `batch_size` in configuration
- Consider using a faster model for title generation
- Check network connectivity to inference gateway

#### Background Jobs Not Running

- Verify persistent storage is enabled and working
- Check logs for background job initialization errors
- Ensure the configured model is available

### Error Handling

The system includes robust error handling:

- Failed title generations are logged but don't stop processing other conversations
- Network errors are retried according to the client retry configuration
- Invalid or empty titles trigger fallback to first-10-words approach
- Storage errors are logged and the system continues with in-memory operation

## API Integration

Title generation integrates with the inference gateway using the same client configuration as other features:

- Respects timeout settings from `client.timeout`
- Uses retry configuration from `client.retry`
- Honors model availability and selection
- Includes middleware for logging and monitoring

## Migration

When upgrading to a version with title generation:

1. **Database Migration**: Schema changes are applied automatically on startup
2. **Existing Conversations**: Will have `title_generated: false` and will be processed by background jobs
3. **Configuration**: Title generation is enabled by default but can be disabled
4. **Backward Compatibility**: All existing functionality continues to work unchanged
