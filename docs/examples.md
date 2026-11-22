# Examples

[← Back to README](../README.md)

**What** - a runnable example per feature plus the common end-to-end workflows.
**Why** - seeing a feature wired up end to end, config and all, is faster than reading a reference and guessing.
**How** - each directory under [`examples/`](../examples/) is self-contained with its own README. Copy the one closest to your setup and adapt it.

## Runnable Examples

Most examples ship a Docker Compose file:

| Example | Demonstrates |
| --------- | -------------- |
| [basic](../examples/basic/) | Minimal gateway + CLI setup to get started |
| [a2a](../examples/a2a/) | Agent-to-Agent: multiple agents, a demo site, and a VNC container |
| [mcp](../examples/mcp/) | MCP server integration with a sample server and config |
| [tools](../examples/tools/) | Custom tools in Python and shell, run offline against a scripted mock model |
| [subagents](../examples/subagents/) | Local subagents: the four built-in agents (`builtins`) with a project `explore.md` shadowing one, a two-task fan-out folded back into one result, and a team whose beta teammate messages its lead mid-run |
| [ag-ui](../examples/ag-ui/) | Consuming the AG-UI event stream with a small Go program, plus a `--serve` worker fed `run_agent_input` frames |
| [scheduling](../examples/scheduling/) | The Schedule tool: the mock model creates a recurring and a one-off cron job, `infer daemon` fires them and records every run, offline against the mock model |
| [heartbeat](../examples/heartbeat/) | The heartbeat: a daemon waking the agent on a short interval and recording each wake-up, plus a single `infer headless --heartbeat` run |
| [sandbox](../examples/sandbox/) | The path policy, the per-mode bash allow-list and the kernel confinement of unattended Bash (Landlock on Linux, Seatbelt on macOS), run offline against a scripted mock model |
| [session-artifacts](../examples/session-artifacts/) | Browsing and saving a session's artifacts from the chat TUI, offline against a scripted mock model |
| [daemon-session-artifacts](../examples/daemon-session-artifacts/) | Two daemons: a session teleported to the second one generates an artifact there, and the chat pulls it home |
| [media-audio](../examples/media-audio/) | Spoken WAV, a composed music loop and a sound effect from the mock gateway's Audio API, offline with no API key |
| [media-video](../examples/media-video/) | Rendering, scoring and dubbing a video clip, plus an avatar from a photo lip-synced over a spoken line, from the mock gateway's Videos API |
| [computer-use](../examples/computer-use/) | Computer Use on a sandboxed desktop container, driven from the web terminal its daemon serves |
| [model-switching](../examples/model-switching/) | Switching models mid-session, with a small frontend |
| [shortcuts](../examples/shortcuts/) | Custom `/`-shortcuts wired through config |
| [skills](../examples/skills/) | Agent Skills: authoring and discovery with scopes, the Skill tool offline against a scripted mock model, and the catalog commands |
| [memory](../examples/memory/) | Persistent memory: the mock model writes a fact in one session, the next session sees the `MEMORY.md` index in its prompt context and reads the fact with the `Memory` tool, then deletes it, and `INFER_MEMORY_ENABLED=false` shows the index and both memory reminders gone |
| [plugins](../examples/plugins/) | Installing a local Claude Code-format plugin under a demo HOME: the registry, plugin skills and prompt injection, the hooks opt-in, then disable and remove |
| [hooks](../examples/hooks/) | A post_session command hook, allow-listed and skipped, plus `reminders.yaml` with `merge: true` and a `--reminders-file` swap, offline against a scripted mock model |
| [web-terminal](../examples/web-terminal/) | The chat TUI in browser tabs served by the daemon, on loopback and over TLS for the LAN |
| [telegram-channel](../examples/telegram-channel/) | Driving the agent from a Telegram channel |
| [working-offline](../examples/working-offline/) | Fully offline usage with local models via Ollama or llama.cpp |
| [gpu-provisioning](../examples/gpu-provisioning/) | Renting an on-demand cloud GPU running llama.cpp via `infer compute gpus` (RunPod) |
| [postgres-storage](../examples/postgres-storage/) | Persisting conversations to PostgreSQL |
| [queue-backends](../examples/queue-backends/) | Every message queue backend (`memory`, `sqlite`, `postgres`, `redis`) plus external producers |
| [a2a-traces](../examples/a2a-traces/) | End-to-end OpenTelemetry traces between the CLI and an A2A agent |
| [a2a-gateway](../examples/a2a-gateway/) | Every A2A agent behind the gateway, addressed by tenant, with a mock model and no API key |
| [a2a-auth](../examples/a2a-auth/) | A2A agents behind a bearer token and behind OIDC with Keycloak, with a mock model and no API key |
| [a2a-auth-gcp](../examples/a2a-auth-gcp/) | An A2A agent behind Google Cloud identity, authenticated with a service account ID token |
| [session-teleport](../examples/session-teleport/) | Teleporting a daemon session to a Kubernetes workspace on k3d (operator Orchestrators in agent-sandbox), with mTLS from the daemon CA, and a desktop workspace whose agent drives its own browser with computer use |
| [kubernetes-session-teleport-netbird](../examples/kubernetes-session-teleport-netbird/) | Teleporting a session from a local daemon to one on k3d whose agent calls a gateway (tokenless, or the Inference Gateway with a real model), every hop on a self-hosted NetBird network behind its own policy and on mutual TLS, with the NetBird operator, a sealed `forward_env`, and an attacker that sees only WireGuard |
| [kubernetes-session-teleport-headscale](../examples/kubernetes-session-teleport-headscale/) | The NetBird example's chain on a self-hosted Headscale tailnet: a local daemon teleports a session to one on k3d whose agent calls a gateway (tokenless, or the Inference Gateway with a real model), each hop admitted by the tailnet policy and on mutual TLS, with tailscale sidecars, Headscale's embedded DERP, a sealed `forward_env`, or with `TAILNET=tailscale` on your own Tailscale account with your machine as the local daemon |
| [a2a-auth-entraid](../examples/a2a-auth-entraid/) | An A2A agent behind Microsoft Entra ID, with the OIDC client-credentials grant and `az` tokens |
| [a2a-auth-aws](../examples/a2a-auth-aws/) | An A2A agent behind Amazon Cognito, authenticated with a machine-to-machine token |
| [a2a-gateway-auth](../examples/a2a-gateway-auth/) | The gateway applying OIDC auth, guardrails and tracing to A2A traffic, with a mock model and no API key |

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
