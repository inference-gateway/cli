# Channels (Remote Messaging)

The Inference Gateway CLI supports pluggable messaging channels that let you
remote-control the agent from external platforms like Telegram or WhatsApp.

## Table of Contents

- [Overview](#overview)
- [Architecture](#architecture)
- [Quick Start (Telegram)](#quick-start-telegram)
- [Configuration](#configuration)
- [Security](#security)
- [Adding a Custom Channel](#adding-a-custom-channel)
- [Supported Channels](#supported-channels)
- [Troubleshooting](#troubleshooting)

## Overview

Channels provide a bidirectional bridge between external messaging platforms
and the CLI agent. When a message arrives from a platform (e.g., Telegram),
it is routed to the agent as a user message. The agent's response is then
sent back through the same platform to the original sender.

Key features:

- **Pluggable**: Add new platforms by implementing a single Go interface
- **Secure by default**: Allowlist-based access control per channel
- **Text and image support**: Forward text messages and images to the agent
- **Video filtering**: Videos are filtered out (text and images only)

## Architecture

```text
                    ┌─────────────────────────────────────────────────┐
                    │                  Inference CLI                   │
                    │                                                  │
                    │  ┌──────────────────┐    ┌──────────────────┐   │
                    │  │ Channel Manager  │    │   Agent Service  │   │
                    │  │                  │    │                  │   │
                    │  │ • Registry       │    │  • Process msg   │   │
┌──────────┐       │  │ • Inbound router ├───▶│  • Tool calls    │   │
│ Telegram │◀─────▶│  │ • Outbound router│◀───┤  • LLM response  │   │
│ Bot API  │       │  │ • Auth check     │    │                  │   │
└──────────┘       │  └──────────────────┘    └──────────────────┘   │
                    │         ▲                        │              │
┌──────────┐       │         │    ┌──────────────┐    │              │
│ WhatsApp │◀─────▶│         └────┤ EventBridge  │◀───┘              │
│ API      │       │              │ (pub-sub)    │                   │
└──────────┘       │              └──────────────┘                   │
                    └─────────────────────────────────────────────────┘
```

**Message flow:**

1. External platform delivers a message to the Channel implementation
2. Channel converts it to an `InboundMessage` and sends to the shared inbox
3. Channel Manager checks the sender against the allowlist
4. If authorized, the message is enqueued via `MessageQueue` for the agent
5. Agent processes the message and produces a `ChatCompleteEvent`
6. Channel Manager subscribes to the `EventBridge`, captures the response
7. Response is routed back through the originating channel via `Channel.Send()`

## Quick Start (Telegram)

### 1. Create a Telegram Bot

1. Open Telegram and message [@BotFather](https://t.me/BotFather)
2. Send `/newbot` and follow the prompts
3. Copy the bot token (e.g., `123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11`)

### 2. Get Your Chat ID

1. Message your new bot in Telegram
2. Visit `https://api.telegram.org/bot<YOUR_TOKEN>/getUpdates`
3. Find your `chat.id` in the response (a numeric ID like `123456789`)

### 3. Configure the CLI

Add to your `.infer/config.yaml`:

```yaml
channels:
  enabled: true

  telegram:
    enabled: true
    bot_token: "${INFER_CHANNELS_TELEGRAM_BOT_TOKEN}"
    allowed_users:
      - "123456789"  # your chat ID
    poll_timeout: 30
```

Or use environment variables:

```bash
export INFER_CHANNELS_ENABLED=true
export INFER_CHANNELS_TELEGRAM_ENABLED=true
export INFER_CHANNELS_TELEGRAM_BOT_TOKEN="123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"
export INFER_CHANNELS_TELEGRAM_ALLOWED_USERS="123456789"
```

### 4. Start the Agent

```bash
infer agent "You are a helpful assistant responding to Telegram messages"
```

### 5. Send a Message

Open Telegram, message your bot, and the agent will respond.

## Configuration

### Full Configuration Reference

```yaml
channels:
  # Master switch for all channels
  enabled: false




  # Telegram Bot API channel
  telegram:
    enabled: false
    bot_token: ""              # Bot token from @BotFather
    allowed_users: []          # List of allowed chat IDs (strings)
    poll_timeout: 30           # Long-polling timeout in seconds

  # WhatsApp Business API channel (Phase 2 - not yet implemented)
  whatsapp:
    enabled: false
    phone_number_id: ""        # Meta Business phone number ID
    access_token: ""           # Meta API access token
    verify_token: ""           # Webhook verification token
    webhook_port: 8443         # Local port for webhook receiver
    allowed_users: []          # List of allowed phone numbers
```

### Environment Variables

All channel settings can be configured via environment variables with the `INFER_` prefix:

| Setting                          | Environment Variable                     |
| -------------------------------- | ---------------------------------------- |
| `channels.enabled`               | `INFER_CHANNELS_ENABLED`                 |
| `channels.telegram.enabled`      | `INFER_CHANNELS_TELEGRAM_ENABLED`        |
| `channels.telegram.bot_token`    | `INFER_CHANNELS_TELEGRAM_BOT_TOKEN`      |
| `channels.telegram.allowed_users`| `INFER_CHANNELS_TELEGRAM_ALLOWED_USERS`  |
| `channels.telegram.poll_timeout` | `INFER_CHANNELS_TELEGRAM_POLL_TIMEOUT`   |

## Security

### Allowlist-Based Access Control

Channels enforce a **secure-by-default** policy:

- If `allowed_users` is empty, **all messages are rejected**
- Only senders whose ID appears in the allowlist can interact with the agent
- Each channel has its own independent allowlist
- Unauthorized attempts are logged for monitoring

### Recommendations

1. **Always set `allowed_users`** - never run with an empty list in production
2. **Use environment variables for tokens** - avoid committing secrets to config files
3. **Use a dedicated bot** - don't reuse bots across projects
4. **Monitor logs** - watch for unauthorized access attempts

## Conversation Memory

Channel messages use the same `ConversationRepository` as interactive chat
and agent sessions. This means:

- **Full conversation history**: The agent automatically includes all prior
  messages when calling the LLM. Your Telegram bot remembers everything
  discussed in the current session.
- **Auto-compaction**: When the conversation grows too long, auto-compact
  summarizes older messages to stay within context limits. This is controlled
  by the existing `compact.auto_at` setting (default: 80% of context window).
- **Persistent storage**: If you use a persistent storage backend (JSONL,
  SQLite, PostgreSQL), conversations survive agent restarts. Resume with
  `--session-id`.
- **No extra configuration needed**: Conversation memory works out of the box
  using the same settings as regular agent/chat sessions.

## Adding a Custom Channel

To add support for a new messaging platform, implement the `domain.Channel` interface:

```go
// Channel represents a pluggable messaging transport
type Channel interface {
    Name() string
    Start(ctx context.Context, inbox chan<- InboundMessage) error
    Send(ctx context.Context, msg OutboundMessage) error
    Stop() error
}
```

### Step-by-Step

1. **Create the implementation** in `internal/services/channels/your_channel.go`:

```go
package channels

type MyChannel struct {
    cfg config.MyChannelConfig
}

func (c *MyChannel) Name() string { return "mychannel" }

func (c *MyChannel) Start(ctx context.Context, inbox chan<- domain.InboundMessage) error {
    // Poll or listen for messages, send to inbox channel
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
            msg := waitForMessage()
            inbox <- domain.InboundMessage{
                ChannelName: "mychannel",
                SenderID:    msg.From,
                Content:     msg.Text,
                Timestamp:   time.Now(),
            }
        }
    }
}

func (c *MyChannel) Send(ctx context.Context, msg domain.OutboundMessage) error {
    // Deliver the response through your platform's API
    return sendToMyPlatform(msg.RecipientID, msg.Content)
}

func (c *MyChannel) Stop() error { return nil }
```

2. **Add config types** to `config/config.go`
3. **Register in the container** in `internal/container/container.go`:

```go
if c.config.Channels.MyChannel.Enabled {
    ch := channels.NewMyChannel(c.config.Channels.MyChannel)
    c.channelManager.Register(ch)
}
```

4. **Add allowlist check** in `channel_manager.go`:

```go
case "mychannel":
    allowedUsers = cm.cfg.MyChannel.AllowedUsers
```

5. **Write tests** in `internal/services/channels/your_channel_test.go`

## Supported Channels

| Channel  | Status    | Transport              | Notes                                 |
| -------- | --------- | ---------------------- | ------------------------------------- |
| Telegram | Available | Long-polling (Bot API) | No webhook needed, works behind NAT   |
| WhatsApp | Planned   | Webhook (Meta Business)| Requires Meta Business account        |
| Discord  | Not yet   | -                      | Contributions welcome                 |
| Slack    | Not yet   | -                      | Contributions welcome                 |

## Troubleshooting

### Messages not arriving

1. Check `channels.enabled: true` and `channels.telegram.enabled: true`
2. Verify the bot token is correct: `curl https://api.telegram.org/bot<TOKEN>/getMe`
3. Ensure your chat ID is in `allowed_users`
4. Check CLI logs for `[channels]` or `[telegram]` entries

### Bot not responding

1. Verify the agent is running (`infer agent ...`)
2. Check that the EventBridge is connected (responses route through it)
3. Look for `[channels] failed to send response` in logs

### Rate limiting

Telegram has rate limits (approximately 30 messages per second). For long
responses, the channel automatically splits messages into 4096-character
chunks.

### Unauthorized user warnings

If you see `[channels] rejected message from unauthorized user`, add the
sender's chat ID to the `allowed_users` list. You can find chat IDs in
the log messages.
