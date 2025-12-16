# Conversation Versioning - Go Back in Time

## Overview

The conversation versioning feature allows you to navigate back to previous points in your
conversation history and restore the conversation to that state.

This is useful when you want to:

- Undo recent mistakes or unwanted changes
- Explore different conversation paths
- Recover from overwhelming token usage
- Return to a specific point in your conversation

## How to Use

### Triggering the Feature

Press **ESC** twice quickly (within 300ms) to open the message history selector.

The feature can be triggered from the chat view when:

- No approval modal is active
- No chat session is currently running (or it's idle/completed)
- You're in any agent mode (Standard, Plan, or Auto-Accept)

### Navigating the Message History

Once the message history selector is open:

1. **Browse messages**: Use arrow keys (↑/↓) to navigate through your previous user messages
2. **Search**: Press `/` to enter search mode and filter messages
3. **Select a restore point**: Press `Enter` to restore the conversation to the selected message
4. **Cancel**: Press `ESC` to exit without making changes

### Message Display Format

Each message in the history is displayed with:

- **Timestamp**: Shows when the message was sent (format: `[HH:MM:SS]`)
- **Message preview**: Truncated to 50 characters with `...` if longer
- **Index**: Position in the conversation history

Example:

```text
[14:23:45] Implement conversation versioning feature...
[14:25:12] Can you add unit tests for the ESC detection?
[14:27:30] Update the documentation with usage examples...
```

### What Happens During Restore

When you select a message to restore:

1. **Deletion**: All messages **after** the selected message are permanently deleted
2. **Auto-save**: The conversation is automatically saved with the changes
3. **Return to chat**: You're immediately returned to the chat input
4. **Ready to continue**: You can continue the conversation from the restore point

**Important**: Deletion is permanent and cannot be undone. Make sure you select the correct restore point.

## Supported Modes

The conversation versioning feature works in all agent modes:

- **Standard Mode**: Full interactive control
- **Plan Mode**: Navigate and restore during planning
- **Auto-Accept Mode**: Quick restore without approval prompts

## Keybinding Configuration

The default keybinding is double ESC (`esc,esc`), but you can customize it:

```bash
# Change the keybinding to a different key combination
infer keybindings set navigation_go_back_in_time "ctrl+h"

# Disable the feature
infer keybindings disable navigation_go_back_in_time

# Re-enable the feature
infer keybindings enable navigation_go_back_in_time

# Reset to default (double ESC)
infer keybindings reset
```

Configuration can also be set in your `.infer/config.yaml`:

```yaml
chat:
  keybindings:
    enabled: true
    bindings:
      navigation_go_back_in_time:
        keys:
          - "esc,esc"  # or your custom keybinding
        description: "go back in time to previous message (double ESC)"
        category: "navigation"
        enabled: true
```

## Edge Cases

### No Messages to Restore

If you trigger the feature when there are no user messages in the conversation:

- A warning is logged: "No user messages to navigate back to"
- The feature exits without opening the selector
- No view transition occurs

### Single User Message

If there's only one user message:

- The selector will show that single message
- You can select it, but no deletion will occur (already at the start)
- This allows you to review the conversation start point

### Empty Conversation

If the conversation is completely empty:

- The feature will not transition to the message history view
- An info message is logged
- You remain in the current view

### Active Chat Session

During active LLM streaming or tool execution:

- Double ESC first cancels the current operation (existing behavior)
- Trigger double ESC again (after cancellation) to open message history
- This prevents accidental navigation during active work

## Limitations

1. **User messages only**: Only user-initiated messages are shown in the history (not assistant responses or tool results)
2. **Permanent deletion**: Messages deleted during restore cannot be recovered
3. **No undo**: There's no undo for the restore operation
4. **Auto-save required**: Changes are automatically saved; manual save is not supported
5. **Single conversation**: Only works within the current conversation context

## Technical Details

### Double ESC Detection

The feature uses timer-based detection:

- Tracks the time between ESC presses
- Triggers when two ESC presses occur within 300ms
- Resets after successful detection or timeout
- Thread-safe implementation with mutex locks

### Message Deletion

Deletion is implemented at the repository level:

- Truncates the message array at the selected index
- Keeps messages from index 0 to the selected index (inclusive)
- Triggers auto-save for persistent storage
- Updates conversation metadata (message count, tokens, etc.)

### State Management

The feature uses dedicated state management:

- `ViewStateMessageHistory`: Separate view state for the selector
- `MessageHistoryState`: Stores user message snapshots
- Clean transitions between chat and history views
- State is cleared after restore or cancellation

## Examples

### Example 1: Undo Recent Mistakes

You've made several changes that you want to undo:

1. Press ESC twice quickly
2. Navigate to the message before your mistakes
3. Press Enter to restore
4. Continue from that point

### Example 2: Explore Different Paths

You want to try a different approach:

1. Press ESC twice to open history
2. Select an earlier message where you want to branch
3. Press Enter to restore
4. Ask a different question to explore an alternative path

### Example 3: Recover from Token Overload

Your conversation has grown too large:

1. Press ESC twice to view history
2. Select a message from earlier in the conversation
3. Press Enter to trim the conversation
4. Continue with a lighter context

## FAQ

**Q: Can I restore deleted messages?**
A: No, deletion is permanent. Always double-check your selection before pressing Enter.

**Q: What happens to tool execution results after restoration?**
A: All messages after the restore point are deleted, including tool results and assistant responses.

**Q: Can I see assistant messages in the history?**
A: No, only user messages are shown. This is intentional to keep the interface focused on user-initiated conversation points.

**Q: Will this affect my saved conversations?**
A: Yes, if auto-save is enabled (which it is by default), the changes are persisted immediately.

**Q: Can I use this feature during tool execution?**
A: Not directly. First cancel the tool execution (single ESC), then trigger the feature (double ESC).

**Q: How many messages can I see in the history?**
A: All user messages from the current conversation are available, with pagination for long histories.

## See Also

- [Keybinding Configuration](../configuration/keybindings.md)
- [Chat Interface Guide](../user-guide/chat-interface.md)
- [Agent Modes](../user-guide/agent-modes.md)
