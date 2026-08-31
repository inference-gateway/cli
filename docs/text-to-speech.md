# Text-to-Speech

The CLI can turn text into spoken audio in two modes:

- **Text to speech** - text in, spoken `.wav` out, using a stock voice.
- **Voice to voice (voice cloning)** - text in plus a short reference recording
  (a ~10-30s `.wav` of the target speaker), spoken WAV out in that voice. This
  is the interesting mode for video-editing and dubbing workflows.

Two synthesis engines are available, selected by `text_to_speech.engine`:

- **`qwen3-tts` (default, direct/local)** - the agent tool shells out to
  llama.cpp's `llama-tts` binary running
  [Qwen3-TTS](https://huggingface.co/ggml-org/Qwen3-TTS-12Hz-1.7B-Base-GGUF)
  GGUF models, the same GGUF ecosystem as whisper.cpp. Fully local, works
  offline, free - but you install and maintain a second inference engine.
- **`gateway`** - synthesis goes through the gateway's Audio API
  (`POST /v1/audio/speech`), using provider-hosted models. Requests appear in
  gateway logs and `infer traces` like any other request, nothing extra to
  install locally. Pick this when your models already run behind the gateway.

The feature is **disabled by default**: while `text_to_speech.enabled` is
false, the `TextToSpeech` tool definition is not sent to the LLM at all, so it
costs zero prompt tokens.

## Gateway engine

```yaml
text_to_speech:
  enabled: true
  engine: gateway
  model: openai/gpt-4o-mini-tts # provider/model, like the image tools
  voice: alloy                  # provider voice id (OpenAI: alloy, echo, nova, ...)
  output_dir: ""                # where generated wavs go; empty = ~/.infer/tts
  require_approval: true        # optional; unset = no approval
```

Only `model`, `voice`, `output_dir` and `require_approval` matter here - the
local-exec keys (`binary_path`, `models_dir`, `auto_download`, `ffmpeg_path`
and the Qwen3 `model` presets) apply to the `qwen3-tts` engine only and are
ignored under `gateway`. Voice cloning still works where the provider supports
it: the `voice_sample` recording is forwarded as a reference sample
(`reference_audio`) for zero-shot cloning; providers without cloning support
(e.g. OpenAI) ignore or reject it.

The CLI-managed local gateway is started with `ENABLE_AUDIO=true` automatically
when this engine is configured. If you point the CLI at an externally managed
gateway, set `ENABLE_AUDIO=true` on it yourself - and note that only providers
with Audio API support (currently OpenAI, or OpenAI-compatible speech backends
via custom provider config) can serve the endpoint.

### Example: voice cloning with llama-server behind the gateway

OpenAI's Speech API has no cloning, but the gateway forwards the request body -
including `reference_audio` - byte-for-byte to the provider, so any
OpenAI-compatible speech backend that supports audio-conditioned cloning works.
llama.cpp's `llama-server` exposes `POST /v1/audio/speech` when started with a
TTS model, and the gateway ships a first-class `llamacpp` provider for it:

```bash
# 1. Serve Qwen3-TTS with llama.cpp (same GGUFs the local engine uses)
llama-server -m Qwen3-TTS-12Hz-1.7B-Base-Q8_0.gguf \
  --mmproj mmproj-Qwen3-TTS-12Hz-1.7B-Base-Q8_0.gguf --port 8081

# 2. Point the gateway at it (for the CLI-managed local gateway, put this in .env)
LLAMACPP_API_URL=http://localhost:8081/v1
```

```yaml
text_to_speech:
  enabled: true
  engine: gateway
  model: llamacpp/qwen3-tts
```

The tool's `voice_sample` recording (a clean mono ~10-30s WAV) is sent as
`reference_audio` and the synthesized speech mimics that speaker - the same
voice-to-voice flow as the local engine, but served over HTTP and visible in
`infer traces`.

The rest of this page covers the local `qwen3-tts` engine.

## Prerequisites (qwen3-tts engine)

Synthesis shells out to external programs (no CGO is added to the `infer`
binary):

| Tool | Used for | Install |
| --- | --- | --- |
| `llama-tts` | Synthesis | Build the `llama-tts` target from [llama.cpp](https://github.com/ggml-org/llama.cpp) or set `text_to_speech.binary_path`. **Needs a build with `qwen3tts` architecture support** - see below |
| `ffmpeg` | Normalizing the voice sample (16kHz mono WAV, capped at 30s) | macOS: `brew install ffmpeg` · Debian/Ubuntu: `apt install ffmpeg` |

ffmpeg is only needed for voice cloning (stock-voice synthesis passes text
straight to `llama-tts`). If ffmpeg is missing and `auto_download` is on, a
prebuilt binary is downloaded into `~/.infer/bin` as a last resort, mirroring
speech-to-text - but that release currently publishes Linux assets only, so on
macOS install ffmpeg yourself (`brew install ffmpeg`). `llama-tts` is never
auto-downloaded on any platform; install it or set `binary_path`. If a required tool is missing, the CLI reports an actionable
error naming what to install - it never fails silently.

Building `llama-tts` from llama.cpp is one cmake invocation, e.g.
`cmake -B build -DGGML_NATIVE=ON && cmake --build build --target llama-tts`.

### llama.cpp version

Qwen3-TTS needs a recent llama.cpp: the build must know the `qwen3tts` model
architecture *and* accept `-mm/--mmproj` for the `llama-tts` tool (upstream
added `LLAMA_EXAMPLE_TTS` to that flag's example list). Older packaged builds -
Homebrew's `llama.cpp` at build `10210`, for instance - satisfy neither and
fail with the two errors listed under Troubleshooting. Build from a current
checkout of master, or upgrade your package (`brew upgrade llama.cpp`), and
verify with:

```console
$ llama-tts --help | grep -- --mmproj
-mm,   --mmproj FILE                    path to a multimodal projector file.
```

## Enabling

Add a `text_to_speech` section to `.infer/config.yaml` (or
`~/.infer/config.yaml`):

```yaml
text_to_speech:
  enabled: true          # feature flag (default: false) - tool absent from the LLM payload when false
  engine: qwen3-tts      # qwen3-tts (default, local) | gateway (see above)
  model: ""              # "" = base preset; q8 | bf16 | or explicit "<backbone>[,<mmproj>].gguf" filenames
  auto_download: true    # download models (and ffmpeg) on first use if missing
  output_dir: ""         # where generated wavs go; empty = ~/.infer/tts
  # Optional overrides:
  binary_path: ""        # explicit llama-tts path; empty = resolve on PATH
  models_dir: ""         # model cache; empty = ~/.infer/models/tts
  timeout: 300           # synthesis timeout (seconds)
  ffmpeg_path: ""        # explicit ffmpeg path; empty = resolve on PATH
  require_approval: true # ask before synthesizing; unset = no approval, like the image tools
```

Every field can also be set via environment variables, e.g.
`INFER_TEXT_TO_SPEECH_ENABLED=true`, `INFER_TEXT_TO_SPEECH_MODEL=q8`,
`INFER_TEXT_TO_SPEECH_REQUIRE_APPROVAL=true`. Leaving `require_approval` unset is
not the same as setting it to `false`: unset keeps the tool's own default (no
approval), an explicit value pins the policy.

## Models

The backbone and mmproj GGUF files are downloaded on first use from
`https://huggingface.co/ggml-org/Qwen3-TTS-12Hz-1.7B-Base-GGUF` and cached
under `~/.infer/models/tts/`. Pick a model with the `model` setting:

| Model | Backbone | Notes |
| --- | --- | --- |
| `""` / `base` (default) | `Qwen3-TTS-12Hz-1.7B-Base-Q4_K_M.gguf` | ~1 GB, good balance |
| `q8` | `Qwen3-TTS-12Hz-1.7B-Base-Q8_0.gguf` | ~1.9 GB, slightly better quality |
| `bf16` | `Qwen3-TTS-12Hz-1.7B-Base-bf16.gguf` | ~3.4 GB, best fidelity |

Each preset downloads the matching `mmproj-*` (audio adapter) file
automatically. You can also pass explicit filenames as
`model: "<backbone>.gguf,<mmproj>.gguf"` (or just the backbone filename; the
`mmproj-<name>-Q8_0.gguf` pair is derived), place both files in `models_dir`
manually, and set `auto_download: false`.

The `llama-tts` binary itself is resolved from `binary_path`, then from
`PATH`. The STT binary release currently hosts no prebuilt `llama-tts`, so
build it once from llama.cpp (or point `binary_path` at your build); if a
prebuilt asset is added later it is downloaded into `~/.infer/bin`
automatically on first use, like ffmpeg.

## Using the agent tool

With `text_to_speech.enabled` set, the agent gains a `TextToSpeech` tool:

- **Stock voice** - ask it to "say X out loud" or "write X as speech to
  say.wav"; the model calls `TextToSpeech` with just `text`.
- **Cloned voice** - give it a reference recording of the target speaker
  (`voice_sample`, a file name inside the working directory), around 10-30
  seconds of clean single-speaker speech. The sample is normalized with ffmpeg
  (16kHz mono, capped at 30s) and passed to the engine's `--tts-speaker-file`
  for zero-shot cloning.
- **Where files go** - `output_path` chooses the destination as a bare file
  name inside `output_dir` (default `~/.infer/tts/`); otherwise a timestamped
  WAV is written there. The result reports the path and audio duration.

Voice cloning quality depends entirely on the reference sample: one speaker,
minimal background noise, no music.

## Troubleshooting

- **"llama-tts binary not found"** - install llama.cpp with TTS support
  (build the `llama-tts` target) or set `text_to_speech.binary_path`.
- **"error: invalid argument: -mm"** - your `llama-tts` predates mmproj support
  for the TTS tool. Upgrade llama.cpp (see [llama.cpp version](#llamacpp-version)).
- **"unknown model architecture: 'qwen3tts'"** - same cause, one layer down:
  the build cannot load the Qwen3-TTS GGUF at all. Upgrade llama.cpp. The
  downloaded models under `~/.infer/models/tts/` are fine and are reused.
- **"ffmpeg not found"** - install ffmpeg or set `text_to_speech.ffmpeg_path`.
- **"tts model ... not found ... auto_download is disabled"** - either enable
  `auto_download` or place the backbone and mmproj GGUFs in `models_dir`.
- **Suspect a corrupt model file** - delete the offending file under
  `~/.infer/models/tts/` to download a fresh copy on the next synthesis.
- **Slow first call** - the models download once (~1.4 GB by default). The
  status line reports download progress while this happens; subsequent runs
  use the cache.
- **Clone sounds wrong** - use a cleaner/longer reference sample (10-30s of
  clean single-speaker speech) and consider the `q8` or `bf16` model preset.
- **Timeouts on long text** - raise `timeout`; long passages synthesize in
  multiple seconds of compute per second of audio depending on your hardware.
