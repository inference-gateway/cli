# GPU provisioning (RunPod)

Rent a serious GPU by the hour, work against it, destroy it when you're done -
you pay only for the hours the pod exists. RunPod Community Cloud lists an
RTX A6000 48GB at roughly $0.33/hr and an RTX PRO 6000 Blackwell 96GB at
roughly $1.69/hr, so occasional heavy sessions cost pocket change compared to
owning the card.

`infer gpu provision` creates the pod running the official
`ghcr.io/ggml-org/llama.cpp:server-cuda` image, which downloads the GGUF you
picked from Hugging Face by itself on boot. To the gateway the pod is just an
ordinary llamacpp provider: the handoff is the standard `LLAMACPP_API_URL` +
`LLAMACPP_API_KEY` env vars - the same two the
[working-offline](../working-offline/) example uses, pointed at the pod's
HTTPS proxy URL instead of localhost.

## Prerequisites

- A [RunPod](https://www.runpod.io) account and API key. The key is
  management-plane only (create/list/destroy calls); the provision form asks
  for it once and stores it as `provisioner.runpod.api_key` in
  `~/.infer/config.yaml`. Alternatively set `INFER_PROVISIONER_RUNPOD_API_KEY`
  (useful in CI) - the form then skips the key question but still asks what
  to provision.
- `infer` installed on the host (the provisioning commands run locally).
- Docker, for the compose path below.

## Provision

```bash
infer gpu provision
```

An interactive form asks what to provision: GPU type (live $/hr pricing,
cheapest first), model as `<hf-repo>:<quant>` (default
`ggml-org/gemma-4-E4B-it-GGUF:Q4_0`), then a confirm step showing the GPU and
price before anything is created. Non-interactive:

```bash
infer gpu provision --gpu-type "NVIDIA GeForce RTX 5090" \
  --model "ggml-org/gemma-4-E4B-it-GGUF:Q4_0" --yes
```

The command streams progress while the pod boots and the model downloads,
then prints the handoff once the server answers:

```text
Ready. Point the gateway at it:
  LLAMACPP_API_URL=https://<pod-id>-8080.proxy.runpod.net/v1
  LLAMACPP_API_KEY=<per-session token>

When done: infer gpu destroy <pod-id>
```

## Connect

**Compose gateway (this directory):**

```bash
cp .env.example .env   # paste the two printed values
docker compose up -d

# Confirm the gateway sees the pod's model
curl -s http://localhost:8080/v1/models

docker compose run --rm cli chat
```

Pick the `llamacpp/...` model in the picker. After editing `.env`, apply it
with `docker compose up -d inference-gateway --force-recreate`.

**Host-managed gateway (no compose):** with `gateway.run: true` (the default)
the CLI starts the gateway itself and passes its environment through, so:

```bash
export LLAMACPP_API_URL=https://<pod-id>-8080.proxy.runpod.net/v1
export LLAMACPP_API_KEY=<per-session token>
infer chat
```

## Cost control

```bash
infer gpu list           # every infer-provisioned pod, from any session
infer gpu status <id>    # state, uptime, $/hr, estimated total so far
infer gpu destroy <id>   # billing stops
```

`list` queries RunPod for pods created by infer (name-prefixed), so a pod
forgotten by a previous session is always visible. Set
`provisioner.max_hourly` in config as a hard price guard - provisioning
refuses anything above it.

Destroying the pod wipes its container disk, including the downloaded GGUF;
the next session re-downloads it on boot. That cold start (a few minutes for
a ~4 GB model) is the accepted trade-off for paying zero while idle.

## Notes

- The pod is reachable only through RunPod's HTTPS proxy
  (`https://<pod-id>-8080.proxy.runpod.net`) and the llama.cpp server requires
  the per-session `--api-key` token - never an open port.
- The proxy has a ~100s idle timeout; streaming responses (the CLI default)
  are unaffected.
- Gated Hugging Face models need an `HF_TOKEN` on the pod - not wired up yet;
  stick to open models like the `ggml-org/*` GGUFs.
