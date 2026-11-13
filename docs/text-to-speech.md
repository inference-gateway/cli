# Text-to-Speech

The CLI can turn text into spoken audio in two modes:

- **Text to speech** - text in, spoken `.wav` out, using a stock voice.
- **Voice to voice (voice cloning)** - text in plus a short reference recording
  (a ~10-30s `.wav` of the target speaker), spoken WAV out in that voice. This
  is the interesting mode for video-editing and dubbing workflows.

Speech always goes through the gateway's Audio API (`POST /v1/audio/speech`).
The model is a `provider/model` id. The default, `local/qwen3-tts`, is the
gateway's built-in engine: llama.cpp's `llama-tts` running
[Qwen3-TTS](https://huggingface.co/ggml-org/Qwen3-TTS-12Hz-1.7B-Base-GGUF),
on the machine the gateway runs on. It needs no provider key, and the gateway
downloads the binary and the models itself. Any other `provider/model` with
Audio API support works too. Requests appear in gateway logs and `infer traces`
like any other request.

The feature is **disabled by default**: while `text_to_speech.enabled` is
false, the `TextToSpeech` tool definition is not sent to the LLM at all, so it
costs zero prompt tokens.

## Enabling

Add a `text_to_speech` section to `.infer/config.yaml` (or
`~/.infer/config.yaml`):

```yaml
text_to_speech:
  enabled: true                 # feature flag (default: false) - tool absent from the LLM payload when false
  model: local/qwen3-tts        # provider/model, e.g. elevenlabs/eleven_v4_turbo or openai/gpt-4o-mini-tts
  voice: ""                     # provider voice id (an ElevenLabs voice id, or OpenAI: alloy, echo, nova, ...); unused by local/qwen3-tts
  auto_download: true           # let the gateway download what local/qwen3-tts needs
  output_dir: ""                # where generated wavs go; empty = <media root>/tts
  require_approval: true        # optional; unset = no approval
```

Every field can also be set via environment variables, e.g.
`INFER_TEXT_TO_SPEECH_ENABLED=true`, `INFER_TEXT_TO_SPEECH_MODEL=local/qwen3-tts`,
`INFER_TEXT_TO_SPEECH_REQUIRE_APPROVAL=true`. Leaving `require_approval` unset is
not the same as setting it to `false`: unset keeps the tool's own default (no
approval), an explicit value pins the policy.

Voice cloning works where the model supports it: the `voice_sample` recording
is sent as a reference sample (`reference_audio`) for zero-shot cloning.
`local/qwen3-tts` clones from it. Providers without cloning support, such as
OpenAI, ignore or reject it.

An ElevenLabs model does not clone from a `voice_sample`. It speaks in `voice`,
which may be the id of a voice you cloned in your ElevenLabs account.

## The gateway's Audio API

The gateway serves `/v1/audio/speech` only when it runs with `AUDIO_ENABLED=true`.

- **The gateway the CLI starts** gets `AUDIO_ENABLED=true` and
  `AUDIO_LOCAL_AUTO_DOWNLOAD=<text_to_speech.auto_download>` automatically while
  `text_to_speech.enabled` is set. The gateway then fetches the `local/qwen3-tts`
  binary and models on first use, about 1.4 GB into `~/.infer/bin/tools` and
  `~/.infer/models/tts`. Requests get a 503 with `Retry-After` while it warms up.
- **A gateway you run yourself** needs `AUDIO_ENABLED=true` set on it. For
  models other than `local/...`, only providers with Audio API support can serve
  the endpoint.

`infer binaries status` and `infer binaries install` manage the same
`~/.infer/bin/tools` cache, so `llama-tts` can be installed ahead of time.

## Using the agent tool

With `text_to_speech.enabled` set, the agent gains a `TextToSpeech` tool:

- **Stock voice** - ask it to "say X out loud" or "write X as speech to
  say.wav". The model calls `TextToSpeech` with just `text`.
- **Cloned voice** - give it a reference recording of the target speaker
  (`voice_sample`, resolved against the working directory first and then the desktop voice samples
  library at `~/.infer/voice/samples`), around 10-30
  seconds of clean single-speaker speech. The sample is sent as it is, so keep
  it short: the gateway refuses a request over its body size limit, 10 MiB by
  default.
- **Where files go** - `output_path` chooses the destination as a bare file
  name inside `output_dir` (default `tts/` under the [media root](directory-structure.md#media-directories)). Otherwise a
  timestamped WAV is written there. The result reports the path and audio
  duration.

Voice cloning quality depends entirely on the reference sample: one speaker,
minimal background noise, no music.

## Sessions on a workspace

A session that teleports to a workspace runs its tools there, so a
`voice_sample` has to exist on that machine. Set `forward_voice_samples: true`
on the workspace entry in `daemon.yaml` and every teleport to it carries this
machine's voice samples library along. The agent then names a sample the same
way as at home. See [infer daemon](daemon.md#workspaces-and-teleports).

## The removed local engine

Earlier versions had a second engine, `text_to_speech.engine: qwen3-tts`, where
the CLI ran `llama-tts` itself. It is gone: the gateway's `local/qwen3-tts`
runs the same model from the same caches.

A config that still names that engine keeps working. `engine` is ignored with a
warning in the log, and so is its `model`, which was a preset of that engine
(`q8`, `bf16`) and not a gateway model - speech uses `local/qwen3-tts`. Remove
`engine`, `binary_path`, `models_dir`, `timeout` and `ffmpeg_path` from the
section, and set `model` to a `provider/model` id.

## Troubleshooting

- **A 404 from `/v1/audio/speech`** - the gateway runs without its Audio API.
  Set `AUDIO_ENABLED=true` on a gateway you run yourself. The one the CLI
  starts is restarted with it automatically.
- **"speech assets are still downloading"** - the gateway is fetching the
  `local/qwen3-tts` binary and models. The first download is about 1.4 GB. Try
  again when it is done.
- **The gateway is killed while speaking** - `local/qwen3-tts` held about 6 GiB
  of memory for one sentence when measured in a container. Give a gateway that
  runs under a memory limit more than that.
- **Suspect a corrupt model file** - delete the offending file under
  `~/.infer/models/tts/` on the gateway's machine to download a fresh copy.
- **Clone sounds wrong** - use a cleaner reference sample (10-30s of clean
  single-speaker speech).
- **Timeouts on long text** - a request waits for `client.timeout` seconds.
  Split a long passage into one call per sentence or two.
