# Channels (Remote Messaging)

The Inference Gateway CLI supports pluggable messaging channels that let you
remote-control the agent from external platforms like Telegram or WhatsApp.

## Table of Contents

- [Overview](#overview)
- [Architecture](#architecture)
- [Quick Start (Telegram)](#quick-start-telegram)
- [Configuration](#configuration)
- [Media Uploads (Telegram)](#media-uploads-telegram)
- [Security](#security)
- [Remote-Control Prompt and System Reminders](#remote-control-prompt-and-system-reminders)
- [Adding a Custom Channel](#adding-a-custom-channel)
- [Supported Channels](#supported-channels)
- [Troubleshooting](#troubleshooting)

## Overview

Channels provide a bridge between external messaging platforms and the CLI
agent. The `infer daemon` command runs as a standalone long-running daemon
that listens for messages from platforms like Telegram. When a message arrives,
it triggers `infer headless --session-id <id>` as a subprocess. The agent
processes the message and the response is sent back through the channel.

Key features:

- **Pluggable**: Add new platforms by implementing a single Go interface
- **Decoupled**: Channel listener is independent from the agent
- **Secure by default**: Allowlist-based access control per channel
- **Persistent sessions**: Deterministic session IDs per sender
- **Text and image support**: Forward text messages and images to the agent
- **Media uploads**: Save inbound photos and videos to a local directory so the
  agent can use them as assets - see [Media Uploads](#media-uploads-telegram)
- **Voice transcription**: Inbound voice messages are transcribed to text with Whisper when
  `speech_to_text.enabled` is set - see [Speech-to-Text](speech-to-text.md)

## Architecture

```text
┌──────────┐       ┌────────────────────────────────────────────────────┐
│ Telegram │◀─────▶│     infer daemon (long-running)   │
│ Bot API  │       │                                                    │
└──────────┘       │     Channel ──▶ inbox ──▶ routeInbound             │
                   │                            │                       │
                   │    ┌───────────────────────▼─────────────┐         │
                   │    │  Per message:                       │         │
                   │    │  1. Check allowlist                 │         │
                   │    │  2. Derive session ID               │         │
                   │    │  3. exec: infer headless               │         │
                   │    │     --session-id channel-telegram-X │         │
                   │    │     "user message"                  │         │
                   │    │  4. Parse JSON stdout               │         │
                   │    │  5. Send response via channel       │         │
                   │    └─────────────────────────────────────┘         │
                   └────────────────────────────────────────────────────┘
```

**Message flow:**

1. External platform delivers a message to the Channel implementation
2. Channel converts it to an `InboundMessage` and sends to the shared inbox
3. Channel Manager checks the sender against the allowlist
4. If authorized, derives a deterministic session ID (e.g.,
   `channel-telegram-123456789`)
5. Triggers `infer headless --session-id <id> "<message>"` as a subprocess
6. Parses the agent's JSON stdout for the assistant response
7. Sends the response back through the originating channel

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

Channel settings live in their own file at `.infer/channels.yaml` (separate
from the main `config.yaml` so bot tokens stay out of the agent's reach -
the file is in `tools.sandbox.protected_paths` by default). `infer init`
seeds it from the in-code defaults; edit it like so:

```yaml
# .infer/channels.yaml
---
enabled: true

telegram:
  enabled: true
  bot_token: "${INFER_CHANNELS_TELEGRAM_BOT_TOKEN}"
  allowed_users:
    - "123456789"  # your chat ID
  poll_timeout: 30
```

Agent-side settings still live in `.infer/config.yaml`:

```yaml
agent:
  model: "openai/gpt-4"
  system_prompt: "You are a helpful assistant"
  custom_instructions: ""  # clear default instructions for lightweight channel use
  max_turns: 1  # recommended for conversational channel use
```

Or use environment variables:

```bash
export INFER_CHANNELS_ENABLED=true
export INFER_CHANNELS_TELEGRAM_ENABLED=true
export INFER_CHANNELS_TELEGRAM_BOT_TOKEN="123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"
export INFER_CHANNELS_TELEGRAM_ALLOWED_USERS="123456789"
export INFER_AGENT_MODEL="openai/gpt-4"
```

### 4. Start the Channel Listener

```bash
infer daemon
```

This starts a long-running daemon that listens for Telegram messages. Each
incoming message triggers a new `infer headless` invocation with a persistent
session per sender.

### 5. Send a Message

Open Telegram, message your bot, and the agent will respond.

## Configuration

### File Layout

Channel settings live in their own file:

- **`.infer/channels.yaml`** - all channel settings (Telegram, WhatsApp,
  max workers, approval flag). Holds bot tokens, so it's listed in
  `tools.sandbox.protected_paths` and the agent cannot read or rewrite it.
- **`.infer/config.yaml`** - agent settings (model, system prompt, etc.).
  Any legacy `channels:` block here is **ignored** at runtime; only
  `channels.yaml` is read. Run `infer init` to migrate an existing block:
  it seeds `channels.yaml` from the loaded values when no
  `channels.yaml` exists yet.

The lookup order for `channels.yaml` is project (`./.infer/channels.yaml`)
first, then userspace (`~/.infer/channels.yaml`).

### Full Configuration Reference

```yaml
# .infer/channels.yaml
---
# Master switch for all channels
enabled: false

# Worker pool size for processing inbound messages
max_workers: 5

# Image retention (number of recent images cached per session)
image_retention: 5

# Require user approval for sensitive tools (default: true)
# When true, tools like Write, Edit, Delete, and Bash will prompt the user
# for approval before executing. Read-only tools (Read, Grep, Tree) are
# not affected. Reuses existing tools.*.require_approval configuration.
require_approval: true

# Telegram Bot API channel
telegram:
  enabled: false
  bot_token: ""              # Bot token from @BotFather
  allowed_users: []          # List of allowed chat IDs (strings)
  poll_timeout: 30           # Long-polling timeout in seconds
  media:
    enabled: false           # Save inbound photos/videos to disk
    dir: ""                  # "" -> ~/.infer/media
    max_size_mb: 10          # Reject downloads larger than this
    retain: 20               # Keep the last N files, oldest pruned
    allowed_mime_types:      # Only these types are saved
      - image/jpeg
      - image/png
      - image/webp
      - video/mp4
      - video/quicktime

# WhatsApp Business API channel (Phase 2 - not yet implemented)
whatsapp:
  enabled: false
  phone_number_id: ""        # Meta Business phone number ID
  access_token: ""           # Meta API access token
  verify_token: ""           # Webhook verification token
  webhook_port: 8443         # Local port for webhook receiver
  allowed_users: []          # List of allowed phone numbers
```

```yaml
# .infer/config.yaml - recommended agent settings for channel use
agent:
  model: "deepseek/deepseek-v4-pro"              # Model to use
  system_prompt: "You are a helpful assistant"  # Base identity
  custom_instructions: ""             # Clear default instructions for lightweight use
  max_turns: 1                        # Single-turn for conversational use
```

### Environment Variables

All channel settings can be configured via environment variables with the
`INFER_` prefix:

| Setting                               | Environment Variable                        |
|---------------------------------------|---------------------------------------------|
| `channels.enabled`                    | `INFER_CHANNELS_ENABLED`                    |
| `channels.require_approval`           | `INFER_CHANNELS_REQUIRE_APPROVAL`           |
| `channels.telegram.enabled`           | `INFER_CHANNELS_TELEGRAM_ENABLED`           |
| `channels.telegram.bot_token`         | `INFER_CHANNELS_TELEGRAM_BOT_TOKEN`         |
| `channels.telegram.allowed_users`     | `INFER_CHANNELS_TELEGRAM_ALLOWED_USERS`     |
| `channels.telegram.poll_timeout`      | `INFER_CHANNELS_TELEGRAM_POLL_TIMEOUT`      |
| `channels.telegram.media.enabled`     | `INFER_CHANNELS_TELEGRAM_MEDIA_ENABLED`     |
| `channels.telegram.media.dir`         | `INFER_CHANNELS_TELEGRAM_MEDIA_DIR`         |
| `channels.telegram.media.max_size_mb` | `INFER_CHANNELS_TELEGRAM_MEDIA_MAX_SIZE_MB` |
| `channels.telegram.media.retain`      | `INFER_CHANNELS_TELEGRAM_MEDIA_RETAIN`      |

## Media Uploads (Telegram)

With `telegram.media.enabled: true`, photos and videos sent to the bot are
saved to a local directory so the agent can use them as assets (edit them,
feed them to other tools, generate content from them):

- Files land in `media.dir` (default `~/.infer/media`) with an
  `infer-media-` prefix; the saved path is appended to the message the agent
  receives (`[Attachment saved: /path/to/infer-media-xxx.mp4]`) and exposed
  as `media_path` metadata.
- **Retention**: only the last `media.retain` files are kept (default 20);
  older ones are pruned automatically.
- **Size limit**: downloads larger than `media.max_size_mb` (default 10) are
  rejected before anything is fetched, and the agent is told why.
- **MIME allowlist**: only `media.allowed_mime_types` are saved (default:
  JPEG/PNG/WebP images and MP4/QuickTime videos).

Photos are still forwarded to the model as vision input regardless of this
setting; enabling it additionally keeps the original files on disk. Videos
are only accepted when this setting is on - otherwise they are skipped.

## Security

### Allowlist-Based Access Control

Channels enforce a **secure-by-default** policy:

- If `allowed_users` is empty, **all messages are rejected**
- Only senders whose ID appears in the allowlist can interact with the agent
- Each channel has its own independent allowlist
- Unauthorized attempts are logged for monitoring

### Recommendations

1. **Always set `allowed_users`** - never run with an empty list in production
2. **Use environment variables for tokens** - avoid committing secrets to
   config files
3. **Use a dedicated bot** - don't reuse bots across projects
4. **Monitor logs** - watch for unauthorized access attempts

## Tool Approval

By default (`channels.require_approval: true`), the channel manager enables
interactive tool approval for sensitive operations. When the agent needs to
execute a tool that requires approval (e.g., Write, Edit, Delete, Bash), it
prompts the channel user and waits for confirmation.

### How It Works

1. The channel manager spawns `infer headless --require-approval`
2. When the agent encounters a tool that requires approval, it outputs a JSON
   approval request on stdout and blocks
3. The channel manager detects the request and sends a human-readable prompt
   to the user (e.g., "Tool approval required: Bash / Command: `rm -rf tmp/`")
4. The user replies **yes** (or y, approve, ok) to approve, or **no** (or n,
   reject) to reject
5. The channel manager writes the approval response to the agent's stdin
6. If no reply is received within **5 minutes**, the tool is automatically
   rejected

### Which Tools Require Approval

This reuses the existing `tools.*.require_approval` configuration:

| Tool      | Default                                           |
|-----------|---------------------------------------------------|
| Bash      | Requires approval (unless command is allowed).    |
| Write     | Requires approval                                 |
| Edit      | Requires approval                                 |
| Delete    | Requires approval                                 |
| Read      | No approval needed                                |
| Grep      | No approval needed                                |
| Tree      | No approval needed                                |
| TodoWrite | No approval needed                                |

You can customize per-tool behavior in `.infer/config.yaml`:

```yaml
tools:
  bash:
    require_approval: true
    mode:
      all:
        allow: ["ls( .*)?", "pwd( .*)?", "git status( .*)?"]
  write:
    require_approval: true
  read:
    require_approval: false
```

### Disabling Tool Approval

To disable approval and auto-execute all tools (original behavior):

```yaml
channels:
  require_approval: false
```

Or: `INFER_CHANNELS_REQUIRE_APPROVAL=false`

## Conversation Memory

Each sender gets a deterministic session ID (e.g.,
`channel-telegram-123456789`). The agent uses `--session-id` to persist
conversations, which means:

- **Full conversation history**: The agent loads all prior messages when
  calling the LLM. Your Telegram bot remembers everything discussed.
- **Auto-compaction**: When the conversation grows too long, auto-compact
  summarizes older messages to stay within context limits. This is controlled
  by the existing `compact.auto_at` setting (default: 80% of context window).
- **Persistent storage**: If you use a persistent storage backend (JSONL,
  SQLite, PostgreSQL), conversations survive restarts.
- **No extra configuration needed**: Conversation memory works out of the box
  using the same settings as regular agent sessions.

## Remote-Control Prompt and System Reminders

Messages routed through the daemon invoke `infer headless --remote`,
which swaps the default agent system prompt for
`prompts.agent.system_prompt_remote`. That prompt is tuned for short,
chat-style replies so that a casual "Hi" does not trigger a paragraph-long
response or unnecessary tool calls.

Customise it in `~/.infer/prompts.yaml`:

```yaml
agent:
  system_prompt_remote: |-
    Remote-control assistant. You are responding through a messaging channel.
    Reply concisely and only use tools when the user asks for work.
```

Or override via environment:

```bash
export INFER_PROMPTS_AGENT_SYSTEM_PROMPT_REMOTE="Pirate-themed assistant."
```

### System Reminders

The CLI can periodically inject a `<system-reminder>` user message into the
conversation to nudge the agent about empty todo lists. Modern LLMs no longer
need this nudge, so it ships **disabled by default**. Re-enable it via
`prompts.yaml`:

```yaml
agent:
  system_reminders:
    enabled: true
    interval: 4  # inject every N turns
    wrap_up_text: |
      Wrap up and commit your work before the session ends.
    wrap_up_threshold: 3  # inject wrap_up_text when max_turns - turns <= 3
```

Or via environment:

```bash
export INFER_PROMPTS_AGENT_SYSTEM_REMINDERS_ENABLED=true
export INFER_PROMPTS_AGENT_SYSTEM_REMINDERS_INTERVAL=4
export INFER_PROMPTS_AGENT_SYSTEM_REMINDERS_WRAP_UP_TEXT="Wrap up and commit your work"
export INFER_PROMPTS_AGENT_SYSTEM_REMINDERS_WRAP_UP_THRESHOLD=3
```

## Adding a Custom Channel

To add support for a new messaging platform, implement the `domain.Channel`
interface:

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

1. **Create the implementation** in
   `internal/presentation/telegram/your_channel.go`:

```go
package channels

type MyChannel struct {
    cfg config.MyChannelConfig
}

func (c *MyChannel) Name() string { return "mychannel" }

func (c *MyChannel) Start(
    ctx context.Context,
    inbox chan<- domain.InboundMessage,
) error {
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

func (c *MyChannel) Send(
    ctx context.Context,
    msg domain.OutboundMessage,
) error {
    // Deliver the response through your platform's API
    return sendToMyPlatform(msg.RecipientID, msg.Content)
}

func (c *MyChannel) Stop() error { return nil }
```

2. **Add config types** to `config/config.go`

3. **Register in the channels command** in `cmd/channels.go`:

```go
if cfg.Channels.MyChannel.Enabled {
    ch := channels.NewMyChannel(cfg.Channels.MyChannel)
    cm.Register(ch)
}
```

4. **Add allowlist check** in `channel_manager.go`:

```go
case "mychannel":
    allowedUsers = cm.cfg.MyChannel.AllowedUsers
```

5. **Write tests** in `internal/presentation/telegram/your_channel_test.go`

## Supported Channels

| Channel  | Status    | Transport               | Notes                               |
|----------|-----------|-------------------------|-------------------------------------|
| Telegram | Available | Long-polling (Bot API)  | No webhook needed, works behind NAT |
| WhatsApp | Planned   | Webhook (Meta Business) | Requires Meta Business account      |
| Discord  | Not yet   | -                       | Contributions welcome               |
| Slack    | Not yet   | -                       | Contributions welcome               |

## Troubleshooting

### Messages not arriving

1. Check `channels.enabled: true` and `channels.telegram.enabled: true`
2. Verify the bot token is correct:
   `curl https://api.telegram.org/bot<TOKEN>/getMe`
3. Ensure your chat ID is in `allowed_users`
4. Check CLI logs for `[channels]` or `[telegram]` entries

### Bot not responding

1. Verify the channel listener is running (`infer daemon`)
2. Check that `infer headless` works independently:
   `infer headless "Hello" --session-id test`
3. Look for `[channels] agent failed` in logs

### Rate limiting

Telegram has rate limits (approximately 30 messages per second). For long
responses, the channel automatically splits messages into 4096-character
chunks.

### Unauthorized user warnings

If you see `[channels] rejected message from unauthorized user`, add the
sender's chat ID to the `allowed_users` list. You can find chat IDs in
the log messages.
