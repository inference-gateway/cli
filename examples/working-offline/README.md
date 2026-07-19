# Working offline

Run `infer` completely offline against a local model - no cloud provider, no
API keys, nothing leaves the machine.

The CLI always talks to an Inference Gateway (it needs the gateway's
provider-prefixed model IDs), so "offline" means: the containerized CLI talks
to a local gateway, which talks to a local model server - Ollama or llama.cpp,
your pick. Everything, including the model download, runs in containers; the
only host requirement is Docker.

## One-time preparation (the only step that needs network)

```bash
cp .env.example .env   # local provider URLs only - nothing to edit
```

**Option A - native Ollama (the default):** containers cannot use the GPU
(notably on macOS), so the default is a model server installed natively on
the host - it runs on the GPU (Metal on Apple Silicon, roughly 10x faster)
while the gateway and CLI stay in containers. `.env.example` already points
the gateway at it via `host.docker.internal`.

```bash
# Install https://ollama.com (or: brew install ollama), then
ollama pull qwen3:8b   # small model with solid tool-calling; any tool-capable model works
docker compose up -d
```

Prefer native llama.cpp instead? `brew install llama.cpp`, run
`llama-server -hf ggml-org/gemma-4-E4B-it-GGUF:Q4_0 --alias gemma-4-e4b --jinja -c 16384`,
and set `LLAMACPP_API_URL=http://host.docker.internal:8080/v1` in `.env`.

**Option B - containerized Ollama (CPU-only):** set
`OLLAMA_API_URL=http://ollama:11434/v1` in `.env`, then:

```bash
docker compose --profile ollama up -d
# Pull the model inside the container, into the ollama_models volume
docker compose exec ollama ollama pull qwen3:8b
```

**Option C - containerized llama.cpp (CPU-only):**

```bash
docker compose --profile llamacpp up -d
```

The llamacpp container downloads the official
[Gemma 4 E4B GGUF](https://huggingface.co/ggml-org/gemma-4-E4B-it-GGUF)
(Q4_0, ~4 GB) from Hugging Face by itself on first start and caches it in
the `llamacpp_cache` volume - no manual download.

To know when the model is ready: the image has a built-in healthcheck, so
`docker compose ps` shows `(health: starting)` while downloading/loading and
`(healthy)` once the server can answer. Watch download progress with
`docker compose logs -f llamacpp`, or poll the gateway - the model appears in
`curl -s http://localhost:8080/v1/models` only when the server is up.

After editing `.env`, apply it with
`docker compose up -d inference-gateway --force-recreate`.

The CLI is agentic, so whatever model you choose must support tool calls
(all of the above do).

## Run offline

```bash
# Confirm the gateway sees your local model
curl -s http://localhost:8080/v1/models

# Chat - fully containerized
docker compose run --rm cli chat
```

The model picker shows `ollama/qwen3:8b` (options A/B) or
`llamacpp/gemma-4-e4b` (option C). Ask it to run a tool ("list the files
here") to see agentic use against the local model.

Expect the first turn to be slow with the containerized backends (B/C): the
agent's ~8k-token system prompt is processed on the CPU before the first token
appears (a minute or more, depending on the machine). Follow-up turns reuse
the prompt cache and respond much faster. The native default (A) runs on the
GPU and responds in seconds.

A host-installed `infer` works too - from this directory:
`INFER_GATEWAY_URL=http://localhost:8080 infer chat`.

## Performance tips for local models

Local prefill is the enemy: the agent sends an ~8k-token system prompt + tool
schemas with every request, so anything that invalidates the model server's
prompt-prefix cache makes a turn cost a full re-read of that prompt.

- The compose file already sets `INFER_AGENT_CONTEXT_GIT_CONTEXT_ENABLED=false`
  and `INFER_AGENT_CONTEXT_TREE_ENABLED=false` - these keep the system prompt
  byte-identical across turns so the cache holds and follow-up turns only pay
  for your new message. Don't remove them for local backends.
- The first turn of a session always pays the full prefill - that one is slow
  by nature; follow-ups should be fast.
- Set expectations by hardware: agentic use means prefilling thousands of
  prompt tokens per turn, so a discrete GPU with high memory bandwidth matters
  more than CPU. On an Apple Silicon laptop (M4 Pro), expect ~1 minute for the
  first turn with an 8B model - workable for occasional offline use, not for
  daily driving.
- Thinking models (like qwen3) spend a long time generating reasoning tokens
  before answering. Set `INFER_AGENT_REASONING_EFFORT=low` (or `minimal`) on
  the `cli` service if your backend honors `reasoning_effort`, or pick a
  non-thinking model.

## What's disabled and why

The CLI is configured entirely through `INFER_*` environment variables on the
`cli` service in `docker-compose.yml` - no config file. They turn off
everything that would otherwise reach the network:

| Env var | Why |
| --- | --- |
| `INFER_GATEWAY_RUN=false` | Compose runs the gateway; the CLI must not download/start one |
| `INFER_TOOLS_WEB_SEARCH_ENABLED=false` | Outbound HTTP to search engines |
| `INFER_TOOLS_WEB_FETCH_ENABLED=false` | Outbound HTTP fetches |
| `INFER_A2A_ENABLED=false` | Would probe remote agent URLs |

Conversation title generation stays on - it uses the same local model.
Conversations persist in the `cli_data` volume.

## Truly air-gapped

After the one-time preparation, images, models, and conversations all live in
local Docker volumes. Disconnect from
the network entirely and `docker compose run --rm cli chat` keeps working;
everything survives `docker compose down` (`down -v` wipes the models and
conversations).
