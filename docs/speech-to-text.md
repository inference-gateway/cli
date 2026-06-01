# Speech-to-Text (Whisper)

The CLI can turn speech into text in two places:

- **Chat mode** — the [`/voice`](shortcuts-guide.md#voice-shortcut) shortcut records your microphone,
  transcribes it, and drops the text into the input field.
- **Channels mode** — inbound **Telegram voice messages** are transcribed to text before the agent
  sees them, so you can talk to the agent from your phone.

Both use a local [whisper.cpp](https://github.com/ggml-org/whisper.cpp) binary and run fully offline
once the model is downloaded. The feature is **disabled by default** because the model download can
take time and the runtime depends on external tools.

## Prerequisites

Speech-to-text shells out to two external programs (no CGO is added to the `infer` binary):

| Tool | Used for | Install |
| --- | --- | --- |
| `whisper-cli` (or `whisper-cpp`) | Transcription | macOS: `brew install whisper-cpp` · Nix: `nix profile install nixpkgs#openai-whisper-cpp` · or build from [whisper.cpp](https://github.com/ggml-org/whisper.cpp) |
| `ffmpeg` | Microphone capture & decoding voice messages (OGG/Opus → WAV) | macOS: `brew install ffmpeg` · Debian/Ubuntu: `apt install ffmpeg` |

On Linux, `arecord` (ALSA) or `sox` can substitute for `ffmpeg` for microphone capture.

If a required tool is missing, the CLI reports an actionable error naming what to install — it never
fails silently.

## Enabling

Add a `speech_to_text` section to `.infer/config.yaml` (or `~/.infer/config.yaml`):

```yaml
speech_to_text:
  enabled: true          # feature flag (default: false)
  engine: whisper.cpp    # transcription engine
  model: tiny            # tiny | base | small | medium | large-v3-turbo | *.en (default: tiny)
  language: ""           # ISO code (e.g. "en"); empty = auto-detect
  auto_download: true    # download the model on first use if missing
  max_recording_seconds: 30  # /voice hard recording cap
  silence_timeout: 2     # stop /voice this many seconds after you go quiet (0 = record full cap)
  # Optional overrides:
  binary_path: ""        # explicit whisper-cli/whisper-cpp path; empty = resolve on PATH
  ffmpeg_path: ""        # explicit ffmpeg path; empty = resolve on PATH
  models_dir: ""         # where models are cached; empty = ~/.infer/models/whisper
  input_device: ""       # microphone device; empty = platform default
  timeout: 120           # transcription timeout (seconds)
```

Every field can also be set via environment variables, e.g.
`INFER_SPEECH_TO_TEXT_ENABLED=true`, `INFER_SPEECH_TO_TEXT_MODEL=base`.

## Models

The GGML model is downloaded on first use from
`https://huggingface.co/ggerganov/whisper.cpp` and cached under `~/.infer/models/whisper/`
(e.g. `ggml-tiny.bin`). Pick a model with the `model` setting; larger models are more accurate but
slower and heavier:

| Model | Size | Notes |
| --- | --- | --- |
| `tiny` | ~75 MB | Fastest, lowest accuracy (default) |
| `base` | ~142 MB | Good balance |
| `small` | ~466 MB | More accurate |
| `medium` | ~1.5 GB | High accuracy |
| `large-v3-turbo` | ~1.5 GB | Best accuracy, optimized speed |

Append `.en` (e.g. `base.en`) for English-only variants. You can also pass a full filename
(`ggml-small.bin`) or place a model in `models_dir` manually and set `auto_download: false`.

## Using `/voice` in chat

1. Type `/voice` and press Enter — recording starts immediately.
2. Speak. Recording stops automatically about `silence_timeout` seconds after you
   go quiet (or at the `max_recording_seconds` cap, or `/voice 8` per-call), and the
   transcription lands in the input field. Set `silence_timeout: 0` to always record
   the full cap instead.
3. Review/edit the text and press Enter to send.

`/voice` only appears when `speech_to_text.enabled` is `true`.

## Telegram voice messages

When `speech_to_text.enabled` is set and you run `infer channels-manager`, voice notes sent to your
Telegram bot are downloaded, decoded with `ffmpeg`, transcribed, and forwarded to the agent as text.
When speech-to-text is disabled, voice messages are ignored (as before). See
[Channels](channels.md) for channel setup.

## Troubleshooting

- **"whisper binary not found"** — install whisper.cpp or set `speech_to_text.binary_path`.
- **"ffmpeg not found"** — install ffmpeg or set `speech_to_text.ffmpeg_path`.
- **No audio captured on macOS** — grant microphone permission to your terminal, and list devices
  with `ffmpeg -f avfoundation -list_devices true -i ""`, then set `input_device` to the index.
- **Wrong language** — set `language` to the ISO code instead of relying on auto-detect.
- **First `/voice` is slow** — the model downloads once; subsequent runs use the cache.
