# Text-to-Music

The CLI can compose a short music clip from a text prompt: a description of the
genre, mood, instruments and tempo in, a `.wav` out. Typical uses are
background music, loops and jingles for video-editing workflows.

Generation goes through the gateway's Music API (`POST /v1/audio/music`). The
CLI holds no provider key; the gateway does, and it routes the request to the
configured `provider/model` (default `elevenlabs/music_v2_5`). Requests appear
in gateway logs and `infer traces` like any other request.

The feature is **disabled by default**: while `text_to_music.enabled` is
false, the `TextToMusic` tool definition is not sent to the LLM at all, so it
costs zero prompt tokens.

## Enabling

Add a `text_to_music` section to `.infer/config.yaml` (or
`~/.infer/config.yaml`):

```yaml
text_to_music:
  enabled: true                 # feature flag (default: false) - tool absent from the LLM payload when false
  model: ""                     # "" = elevenlabs/music_v2_5; or any provider/model the gateway serves
  output_dir: ""                # where generated wavs go; empty = ~/.infer/tmp/music
  require_approval: false       # optional; unset = no approval, like the image tools
```

Every field can also be set via environment variables, e.g.
`INFER_TEXT_TO_MUSIC_ENABLED=true`, `INFER_TEXT_TO_MUSIC_MODEL=elevenlabs/music_v2_5`,
`INFER_TEXT_TO_MUSIC_REQUIRE_APPROVAL=true`. Leaving `require_approval` unset is
not the same as setting it to `false`: unset keeps the tool's own default (no
approval), an explicit value pins the policy.

`model` must be of the form `provider/model`; config validation rejects a bare
model name as soon as the feature is enabled.

## Gateway requirements

The music endpoint is part of the gateway's Audio API, so the gateway must run
with `AUDIO_ENABLED=true` and hold credentials for the provider behind `model`
(for the default, an ElevenLabs API key). The CLI-managed local gateway is
started with `AUDIO_ENABLED=true` automatically when `text_to_music.enabled`
is on, and an already-running instance without the Audio API is restarted.
If you point the CLI at an externally managed gateway, set `AUDIO_ENABLED=true`
and the provider key on it yourself.

A gateway without the endpoint, or a provider that rejects the request, makes
the tool call fail with a one-line error naming the configured model
(`music generation with elevenlabs/music_v2_5 failed: ...`). The agent run
still completes, and no partial WAV is left behind.

## Using the agent tool

With `text_to_music.enabled` set, the agent gains a `TextToMusic` tool:

- **Prompt** - ask for "a calm 30 second lo-fi piano loop" or "an upbeat
  synth-pop jingle with no vocals"; the model calls `TextToMusic` with a
  `prompt` describing genre, mood, instruments and tempo.
- **Length and vocals** - `seconds` caps the clip length; omitted lets the
  provider pick a length that fits the prompt. `instrumental: true` guarantees
  the clip has no vocals.
- **Where files go** - `output_path` chooses the destination as a bare file
  name inside `output_dir` (default `~/.infer/tmp/music/`); otherwise a
  timestamped `music-*.wav` is written there. The result reports the path and
  audio duration.

The clip is always written as WAV. To place it elsewhere, compose first and
copy the returned file.

## Troubleshooting

- **"music generation with ... failed: 404"** - the gateway is not serving
  `/v1/audio/music`. See [Gateway requirements](#gateway-requirements).
- **"invalid text_to_music.model"** - set `model` as `provider/model`, e.g.
  `elevenlabs/music_v2_5`.
- **Temporarily unavailable / slow first call** - music models are slow;
  wait a few seconds and retry once.
- **Provider rejects the request** - check the provider credentials on the
  gateway and that the model supports the requested duration.
