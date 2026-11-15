# Media tools - audio

The three Audio API tools, each off by default behind its own switch: `TextToSpeech` synthesizes a spoken WAV,
`TextToMusic` composes a loop and `TextToSFX` generates a one-shot effect or ambience. See
[Text-to-Speech](../../docs/text-to-speech.md) and [Text-to-Music](../../docs/text-to-music.md).

The model is a [tokenless](https://github.com/inference-gateway/tokenless) mock scripted in
[`scenarios.yaml`](scenarios.yaml) that answers the gateway's `POST /v1/audio/speech`, `POST /v1/audio/music` and
`POST /v1/audio/sfx` endpoints with canned clips, so the example runs offline with no API key and no gateway. You need
[Task](https://taskfile.dev) to run it.

## Run it

`task` lists the scenarios. Each one runs `infer headless` against the mock model and keeps only the `"role":"tool"`
line of its JSON output, which names the saved file.

| Task | What happens |
| --- | --- |
| `task speech` | The model calls `TextToSpeech` and the mock's speech endpoint answers a WAV saved as `greeting.wav` |
| `task music` | The model calls `TextToMusic` and the mock's music endpoint answers an MP3 loop saved as `loop.mp3` |
| `task sfx` | The model calls `TextToSFX` and the mock's SFX endpoint answers an MP3 door knock saved as `knock.mp3` |
| `task all` | Runs the three scenarios above |

Clips land under the media directories on this machine (`tts/`, `music/`, `sfx/`, see
[Directory Structure](../../docs/directory-structure.md)) - the tool result names the exact path.

### What to expect

`task speech`:

```text
{"content":"{\"tool_name\":\"TextToSpeech\", \"success\":true, ... \"path\":\"<media root>/tts/greeting.wav\"}", ...}
```

## Without Task

The Taskfile only sets the environment. Export it yourself and run `infer` from this directory:

```bash
export INFER_GATEWAY_MOCK=true
export INFER_GATEWAY_MOCK_SCENARIOS="$PWD/scenarios.yaml"
export INFER_AGENT_MODEL=mock/openai/gpt-4o
export INFER_TEXT_TO_SPEECH_ENABLED=true
export INFER_TEXT_TO_MUSIC_ENABLED=true
export INFER_TEXT_TO_SFX_ENABLED=true

infer headless --no-save "say good morning as speech, saved as greeting.wav" | grep '"role":"tool"'
```

## With a real model

Drop `INFER_GATEWAY_MOCK`, `INFER_GATEWAY_MOCK_SCENARIOS` and `INFER_AGENT_MODEL` to talk to a real gateway. The
three switches stay. The gateway needs its Audio API (`AUDIO_ENABLED=true`, set for you while any switch above is on)
and the API key of the provider behind each model - the CLI holds no provider key, the gateway does:

| Tool | Default model | Provider key the gateway needs |
| --- | --- | --- |
| `TextToSpeech` (`text_to_speech.model`) | `local/qwen3-tts` | none - the gateway's built-in engine, it fetches its weights itself |
| `TextToMusic` (`text_to_music.model`) | `elevenlabs/music_v2_5` | an ElevenLabs API key |
| `TextToSFX` (`text_to_sfx.model`) | `elevenlabs/eleven_text_to_sound_v2` | an ElevenLabs API key |

Swap a model with `INFER_TEXT_TO_SPEECH_MODEL`, `INFER_TEXT_TO_MUSIC_MODEL` or `INFER_TEXT_TO_SFX_MODEL` - any
`provider/model` the gateway serves. See the [Text-to-Speech](../../docs/text-to-speech.md) and
[Text-to-Music](../../docs/text-to-music.md) docs for the gateway-side details.

Video and avatars live in the sibling example, [media-video](../media-video/).
