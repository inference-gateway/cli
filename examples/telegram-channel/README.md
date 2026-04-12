# Telegram Channel Example (with A2A Web Browsing)

Control the Inference Gateway agent remotely from Telegram. Send text messages or images to a Telegram bot, and the agent responds through the same chat.

This example also bundles an **A2A browser agent**, so you can ask the bot to surf the web
for you - e.g. *"open news.ycombinator.com and tell me the top 3 stories"* - straight from
your phone. A VNC viewer is included so you can watch the browser navigate live.

## What's Included

- **inference-gateway** - routes LLM requests to your configured providers
- **infer-channels-manager** - long-polls Telegram, spawns `infer agent` per message
- **browser-agent** - A2A service running Playwright in headed mode under Xvfb
- **browser-vnc** *(optional, `--profile vnc`)* - `x11vnc` bridge so you can watch the browser at `vnc://localhost:5900`

## Prerequisites

1. A Telegram account
2. An API key from at least one LLM provider (OpenAI, Anthropic, etc.)

## Setup

### 1. Create a Telegram Bot

1. Open Telegram and message [@BotFather](https://t.me/BotFather)
2. Send `/newbot`
3. Follow the prompts to name your bot
4. Copy the **bot token** (looks like `123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11`)

### 2. Get Your Chat ID

1. Send any message to your new bot in Telegram
2. Open this URL in your browser (replace `<TOKEN>` with your bot token):

   ```text
   https://api.telegram.org/bot<TOKEN>/getUpdates
   ```

3. Find your chat ID in the JSON response:

   ```json
   "chat": {"id": 123456789, ...}
   ```

### 3. Configure Environment

```bash
cp .env.example .env
```

Edit `.env` and fill in:

- Your LLM provider API key(s)
- `INFER_CHANNELS_TELEGRAM_BOT_TOKEN` - the bot token from step 1
- `INFER_CHANNELS_TELEGRAM_ALLOWED_USERS` - your chat ID from step 2
- `INFER_AGENT_MODEL` - the model to use (e.g., `openai/gpt-4`)

### 4. Start

```bash
docker compose up -d
```

### 5. Chat

Open Telegram and send a message to your bot. The agent will respond.

### 6. Try Web Browsing

Send any of these to your bot:

- *"Use the browser agent to open <https://example.com> and tell me the page title."*
- *"Browse to <https://news.ycombinator.com> and list the top 5 story titles."*
- *"Find the current weather in Berlin from a public website."*

The channels-manager has `INFER_A2A_ENABLED=true` and the browser agent
registered, so the spawned `infer agent` process will pick `A2A_SubmitTask`
and delegate the browsing task to the `browser-agent` container.

### Watching the Browser Live (Optional)

The browser-agent runs Playwright in headed mode inside Xvfb. To watch what
it's doing, start the optional `browser-vnc` service via the `vnc` profile:

```bash
docker compose --profile vnc up -d browser-vnc
```

Then connect a VNC client to `localhost:5900` (password: `password`):

```bash
# macOS Screen Sharing
open vnc://localhost:5900

# Or any VNC client (RealVNC, TigerVNC, etc.) → localhost:5900
```

Send a browsing prompt from Telegram and watch the browser navigate in
real time. The VNC service is opt-in because it requires building a small
image - the bot itself works without it.

## How It Works

```text
You (Telegram) --> Telegram Bot API --> infer channels-manager (listener)
                                            |
                                            v
                                     infer agent --session-id <id>
                                            |
                                            v  (A2A_SubmitTask)
                                     browser-agent --> Playwright --> the web
                                            |
                                            v
                                     Inference Gateway --> LLM Provider
                                            |
                                            v
                                     JSON stdout (parsed)
                                            |
                                            v
                                     infer channels-manager --> Telegram Bot API --> You (Telegram)
```

1. You send a message in Telegram
2. The channel listener (long-polling) picks it up
3. The message is checked against `allowed_users`
4. If authorized, `infer agent --session-id <id>` is triggered as a subprocess
5. The agent processes it (may use tools, call LLM)
6. The response is parsed from stdout and sent back through Telegram

## Running Without Docker

```bash
# Set environment variables
export INFER_CHANNELS_ENABLED=true
export INFER_CHANNELS_TELEGRAM_ENABLED=true
export INFER_CHANNELS_TELEGRAM_BOT_TOKEN="your-bot-token"
export INFER_CHANNELS_TELEGRAM_ALLOWED_USERS="your-chat-id"
export INFER_AGENT_MODEL="openai/gpt-4"

# Start the channel listener
infer channels-manager
```

## Security

- **Only whitelisted users can interact** - messages from unknown chat IDs are silently rejected
- **Empty allowlist = reject all** - if you forget to set `allowed_users`, nobody can use the bot
- **Never commit `.env`** - it contains your bot token and API keys
- The `.env` file is in `.gitignore` by default
- The `browser-agent` is only reachable inside the compose network, but the
  VNC port `5900` is exposed on `localhost`. Don't run this on a shared host
  without firewalling that port (or remove the `ports:` mapping on
  `browser-vnc`)

## Multiple Users

Add multiple chat IDs to allow more users:

```bash
# In .env
INFER_CHANNELS_TELEGRAM_ALLOWED_USERS="123456789,987654321"
```

Or in `config.yaml`:

```yaml
channels:
  telegram:
    allowed_users:
      - "123456789"
      - "987654321"
```

## Troubleshooting

### Bot not responding

```bash
# Check channel listener logs
docker compose logs infer-channels-manager

# Verify bot token is valid
curl https://api.telegram.org/bot<TOKEN>/getMe
```

### "Unauthorized user" in logs

Your chat ID is not in `allowed_users`. Check the logs for the rejected chat ID and add it to your `.env`.

### Gateway connection errors

```bash
# Check gateway is running
docker compose logs inference-gateway
curl http://localhost:8080/health
```

## Cleanup

```bash
docker compose down
```
