# Inference Gateway UI

Web interface for the Inference Gateway CLI, featuring direct database access and headless CLI session management using the AG-UI protocol.

## Features

- **AG-UI Protocol Compliant**: Headless CLI sessions via stdin/stdout using the [AG-UI Protocol](https://docs.ag-ui.com)
- **Direct Database Access**: TypeScript storage client mirrors CLI's Go storage layer
- **Multiple Storage Backends**: PostgreSQL, SQLite, Redis, JSONL, In-Memory
- **Concurrent Chat Sessions**: Spawn and manage multiple headless CLI processes
- **Real-time Streaming**: Live streaming responses from LLMs
- **Multimodal Support**: Text and image inputs
- **Conversation Management**: Browse, search, and export conversations
- **Analytics Dashboard**: Token usage and cost statistics

## Architecture

```text
┌─────────────────────────────────────────────────────────┐
│                      UI (Next.js)                        │
│  ┌────────────────────┐      ┌─────────────────────┐   │
│  │ TypeScript Storage │      │  CLI Process Mgr    │   │
│  │ Client (PG/SQLite/ │      │  (spawn headless    │   │
│  │  Redis/JSONL)      │      │   CLI sessions)     │   │
│  └──────────┬─────────┘      └──────────┬──────────┘   │
└─────────────┼────────────────────────────┼──────────────┘
              │                            │
              │ Direct DB Access           │ AG-UI Protocol
              │                            │ (stdin/stdout JSON)
┌─────────────▼────────────────────────────▼──────────────┐
│                   Shared Database                        │
│         (PostgreSQL / SQLite / Redis / JSONL)            │
└──────────────────────────┬───────────────────────────────┘
                           │
              ┌────────────┴─────────────┐
              │                          │
┌─────────────▼──────────┐  ┌───────────▼──────────┐
│ CLI Session 1          │  │ CLI Session 2         │
│ (headless, UI-spawned) │  │ (headless, UI-spawned)│
└────────────────────────┘  └───────────────────────┘
```

## Prerequisites

- Node.js 20+ or 22+
- Inference Gateway CLI installed (`infer` binary in PATH or specified location)
- Database setup (PostgreSQL, SQLite, Redis, or JSONL directory)

## Installation

```bash
cd ui
npm install
```

## Configuration

Create a `.env.local` file:

```env
# Storage Configuration (choose one)
NEXT_PUBLIC_STORAGE_TYPE=postgres

# PostgreSQL
NEXT_PUBLIC_STORAGE_POSTGRES_HOST=localhost
NEXT_PUBLIC_STORAGE_POSTGRES_PORT=5432
NEXT_PUBLIC_STORAGE_POSTGRES_DB=infer
NEXT_PUBLIC_STORAGE_POSTGRES_USER=postgres
NEXT_PUBLIC_STORAGE_POSTGRES_PASSWORD=password

# SQLite
NEXT_PUBLIC_STORAGE_SQLITE_PATH=/Users/username/.infer/conversations.db

# Redis
NEXT_PUBLIC_STORAGE_REDIS_HOST=localhost
NEXT_PUBLIC_STORAGE_REDIS_PORT=6379
NEXT_PUBLIC_STORAGE_REDIS_DB=0

# JSONL
NEXT_PUBLIC_STORAGE_JSONL_DIR=/Users/username/.infer/conversations

# CLI Path
NEXT_PUBLIC_CLI_PATH=/usr/local/bin/infer
```

## Development

```bash
npm run dev
```

Open [http://localhost:3000](http://localhost:3000)

## Production Build

```bash
npm run build
npm run start
```

## Project Structure

```text
ui/
├── app/                    # Next.js 16 app directory
│   ├── layout.tsx          # Root layout
│   ├── page.tsx            # Home page
│   ├── providers.tsx       # React Query provider
│   ├── chat/               # Live chat page
│   ├── conversations/      # Conversation browser page
│   └── dashboard/          # Analytics dashboard page
├── components/             # React components
│   ├── chat/               # Chat UI components
│   ├── conversations/      # Conversation list/detail components
│   └── stats/              # Dashboard components
├── lib/                    # Core libraries
│   ├── storage/            # TypeScript storage client
│   │   ├── interfaces.ts   # Type definitions
│   │   ├── factory.ts      # Storage factory
│   │   ├── hooks.ts        # React Query hooks
│   │   ├── postgres/       # PostgreSQL implementation
│   │   ├── sqlite/         # SQLite implementation
│   │   ├── redis/          # Redis implementation
│   │   ├── jsonl/          # JSONL implementation
│   │   └── memory/         # In-memory implementation
│   └── cli/                # CLI process manager
│       ├── process-manager.ts  # Session management
│       ├── hooks.ts        # React hooks
│       └── index.ts        # Exports
└── package.json
```

## AG-UI Protocol

The UI communicates with headless CLI sessions using the [AG-UI Protocol](https://docs.ag-ui.com):

### Event Types (CLI → UI)

**Run Lifecycle:**

- `RunStarted`: Agent run begins
- `RunFinished`: Agent run completes
- `RunError`: Error during run

**Text Messages:**

- `TextMessageStart`: Start of streaming message
- `TextMessageContent`: Delta content chunk
- `TextMessageEnd`: End of streaming message

**Tool Execution:**

- `ToolCallStart`: Tool invocation begins (includes `status` and `metadata` fields)
- `ToolCallArgs`: Tool arguments
- `ToolCallEnd`: Tool call ready
- `ToolCallProgress`: Intermediate progress updates during execution (NEW)
- `ToolCallResult`: Tool execution result (includes `status`, `duration`, and `metadata` fields)
- `ParallelToolsMetadata`: Summary metadata for parallel tool execution (NEW)

### Input Types (UI → CLI)

- `message`: User message with optional images
- `interrupt`: Stop current processing
- `shutdown`: Graceful shutdown

### Tool Execution Progress Events (NEW)

The AG-UI protocol now includes real-time progress tracking for tool execution:

**ToolCallStart** (Enhanced):

```json
{
  "type": "ToolCallStart",
  "toolCallId": "call_abc123",
  "toolCallName": "Bash",
  "parentMessageId": "msg_xyz789",
  "status": "queued",
  "timestamp": 1703001234567
}
```

**ToolCallProgress** (New):

```json
{
  "type": "ToolCallProgress",
  "toolCallId": "call_abc123",
  "status": "running",
  "message": "Executing command...",
  "output": "total 48\ndrwxr-xr-x  12 user  staff   384 Dec 21 10:30 .\n",
  "metadata": {
    "isComplete": false
  },
  "timestamp": 1703001235123
}
```

**ToolCallResult** (Enhanced):

```json
{
  "type": "ToolCallResult",
  "messageId": "msg_result_456",
  "toolCallId": "call_abc123",
  "content": "Command executed successfully",
  "role": "tool",
  "status": "complete",
  "duration": 2.34,
  "timestamp": 1703001236567
}
```

**ParallelToolsMetadata** (New):

```json
{
  "type": "ParallelToolsMetadata",
  "totalCount": 3,
  "successCount": 2,
  "failureCount": 1,
  "totalDuration": 5.67,
  "timestamp": 1703001240000
}
```

**Status Values:**

- `queued`: Tool is queued for execution
- `running`: Tool is actively executing
- `complete`: Tool execution completed successfully
- `failed`: Tool execution failed with error

## Usage Examples

### Starting a Chat Session

```typescript
import { useCLISession } from "@/lib/cli/hooks";

function ChatPage() {
  const { start, sendMessage, messages } = useCLISession();

  useEffect(() => {
    start();
  }, []);

  const handleSend = () => {
    sendMessage({ content: "Hello!" });
  };

  // Render chat UI
}
```

### Accessing Storage

```typescript
import { useConversations } from "@/lib/storage/hooks";

function ConversationsList() {
  const { data: conversations } = useConversations(50, 0);

  // Render conversations
}
```

## Extracting to Separate Repository

This UI is currently embedded in the CLI repository for convenience. To extract:

```bash
# From CLI repo root
cp -r ui /path/to/new/ui-repo
cd /path/to/new/ui-repo
npm install
```

Update paths if needed and ensure `.env.local` points to correct CLI binary and database.

## License

Same as Inference Gateway CLI

## Links

- [AG-UI Protocol Documentation](https://docs.ag-ui.com)
- [Inference Gateway CLI](https://github.com/inference-gateway/cli)
- [Next.js 16 Documentation](https://nextjs.org/docs)
