# Examples

[← Back to README](../README.md)

**What** - a runnable example per feature plus the common end-to-end workflows.
**Why** - seeing a feature wired up end to end, config and all, is faster than reading a reference and guessing.
**How** - each directory under [`examples/`](../examples/) is self-contained with its own README; copy the one closest to your setup and adapt it.

## Runnable Examples

Most examples ship a Docker Compose file:

| Example | Demonstrates |
| --------- | -------------- |
| [basic](../examples/basic/) | Minimal gateway + CLI setup to get started |
| [a2a](../examples/a2a/) | Agent-to-Agent: multiple agents, a demo site, and a VNC container |
| [mcp](../examples/mcp/) | MCP server integration with a sample server and config |
| [tools](../examples/tools/) | Custom tools in Python and shell, run offline against a scripted mock model |
| [computer-use](../examples/computer-use/) | Computer Use driving a sandboxed Ubuntu GUI container |
| [model-switching](../examples/model-switching/) | Switching models mid-session, with a small frontend |
| [shortcuts](../examples/shortcuts/) | Custom `/`-shortcuts wired through config |
| [web-terminal](../examples/web-terminal/) | Browser-based, multi-tab web terminal |
| [telegram-channel](../examples/telegram-channel/) | Driving the agent from a Telegram channel |
| [working-offline](../examples/working-offline/) | Fully offline usage with local models via Ollama or llama.cpp |
| [gpu-provisioning](../examples/gpu-provisioning/) | Renting an on-demand cloud GPU running llama.cpp via `infer gpu` (RunPod) |
| [postgres-storage](../examples/postgres-storage/) | Persisting conversations to PostgreSQL |
| [a2a-traces](../examples/a2a-traces/) | End-to-end OpenTelemetry traces between the CLI and an A2A agent |
| [a2a-gateway](../examples/a2a-gateway/) | Every A2A agent behind the gateway, addressed by tenant, with a mock model and no API key |

There is also an [examples/kubernetes](../examples/kubernetes/) manifest set for running the gateway and an
agent on a cluster.

## Working on a GitHub Issue

```bash
# Start chat
infer chat

# In chat, use shortcuts to get context
/scm issue 123

# Discuss with AI, let it use tools to:
# - Read files
# - Search codebase
# - Make changes
# - Run tests

# Ask the agent to open the PR when ready
> Create a pull request for these changes - it fixes the authentication timeout issue
```

## Related

- [Quick Start](../README.md#quick-start) - first chat in three commands
- [Commands Reference](commands-reference.md) - the main commands and the global flags
- [Directory Structure](directory-structure.md) - what the CLI writes where
