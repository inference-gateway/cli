# Text-to-Video

The CLI can render a short video clip in two modes:

- **Text to video** - a text prompt in, an `.mp4` out: b-roll, establishing shots, simple animated scenes.
- **Lip-synced avatar** - a portrait plus an audio clip in, a talking clip out: the portrait is animated to match the voice. This is the face
  half of the desktop's Content project workflow; `TextToSpeech` is the voice half.

Generation goes through the gateway's unified Videos API (`POST /v1/videos`, `GET /v1/videos/{id}`, `GET /v1/videos/{id}/content`, gateway v0.54.0+).
The CLI holds no provider key; the gateway does, and it routes the job to the configured `provider/model` (default `elevenlabs/creatify-aurora`).
Requests appear in gateway logs and `infer traces` like any other request.

The feature is **disabled by default**, and for more than the usual zero-prompt-token reason: avatar renders upload the user's face and voice to a
third-party provider. While `text_to_video.enabled` is false, the `TextToVideo` tool definition is not sent to the LLM at all.

## Enabling

Add a `text_to_video` section to `.infer/config.yaml` (or `~/.infer/config.yaml`):

```yaml
text_to_video:
  enabled: true            # feature flag (default: false) - tool absent from the LLM payload when false
  model: ""                # "" = elevenlabs/creatify-aurora; or any provider/model the gateway serves
  size: ""                 # optional widthxheight passthrough, e.g. 720x1280; empty = provider default
  output_dir: ""           # where generated mp4s go; empty = ~/.infer/tmp/video
  timeout: 900             # whole-render timeout (seconds): create, poll and download
  poll_interval: 5         # job status poll cadence (seconds)
  require_approval: false  # optional; unset = no approval, like the image tools
```

Every field can also be set via environment variables, e.g. `INFER_TEXT_TO_VIDEO_ENABLED=true`,
`INFER_TEXT_TO_VIDEO_MODEL=elevenlabs/creatify-aurora`, `INFER_TEXT_TO_VIDEO_TIMEOUT=600`. Leaving `require_approval` unset is
not the same as setting it to `false`: unset keeps the tool's own default (no approval), an explicit value pins the policy.

`model` must be of the form `provider/model`; config validation rejects a bare model name as soon as the feature is enabled.

## Size rules

`size` is a passthrough of the gateway's `widthxheight` form - the CLI neither maps nor validates resolutions. For the default
`elevenlabs/creatify-aurora` the gateway derives the resolution from the shorter side (480, 720 or 1080) and the aspect ratio from the
reduced ratio (`16:9`, `9:16`, `1:1`), and rejects anything else:

| `size` | Render |
| --- | --- |
| `1280x720` | landscape 16:9, 720p |
| `720x1280` | portrait 9:16, 720p |
| `480x480` | square 1:1, 480p |

A gateway that cannot map a size rejects the job and the tool surfaces the provider's error message.

## Gateway requirements

The Videos API is part of the gateway (v0.54.0+) and needs no extra feature flag; the gateway must hold credentials for the provider
behind `model` (for the default, an ElevenLabs API key). If you point the CLI at an externally managed gateway, make sure it is on
v0.54.0 or later and holds the provider key.

A gateway without the endpoint, or a provider that rejects the request, makes the tool call fail with a one-line error naming the
configured model (`video creation with elevenlabs/creatify-aurora failed: ...`). The agent run still completes, and no partial file
is left behind.

## Using the agent tool

With `text_to_video.enabled` set, the agent gains a `TextToVideo` tool:

- **Text-only clip** - ask for "a slow drone shot over neon rooftops"; the model calls `TextToVideo` with a `prompt`, optionally
  `seconds` and `size`. `seconds` is a passthrough string (providers accept a limited set of values); the gateway rejects unsupported ones.
- **Lip-synced avatar** - give it a portrait (`avatar`, a bare image name: working directory first, then the avatar library
  `~/.infer/models/avatars/`) and a driving clip (`audio`, a bare `.wav` or `.mp3` name: working directory first, then the
  `TextToSpeech` output directory - so a just-generated `TextToSpeech` WAV can be lip-synced in the next tool call). `prompt` then
  describes framing only; the dialogue comes from the audio. `seconds` is ignored for avatar renders.
- **Where files go** - `output_path` chooses the destination as a bare file name inside `output_dir` (default `~/.infer/tmp/video/`);
  otherwise a timestamped `video-*.mp4` is written there. The result reports the path, model, size and which avatar/audio were used.

The clip is always written as MP4 (`video/mp4` for creatify-aurora). To place it elsewhere, compose first and copy the returned file.

## Upload limit

The avatar portrait and the driving audio travel in one multipart request body, and the gateway caps request bodies at 10 MiB by
default. The tool checks the combined size before sending and rejects bigger uploads with a one-line error suggesting a shorter clip
or an MP3. A `TextToSpeech` WAV at 24 kHz mono is roughly 3 MB per minute, so typical clips fit; for longer audio prefer MP3.

## Troubleshooting

- **"video creation with ... failed: 404"** - the gateway is not serving `/v1/videos` (pre-v0.54 gateway). Upgrade the gateway.
  See [Gateway requirements](#gateway-requirements).
- **"invalid text_to_video.model"** - set `model` as `provider/model`, e.g. `elevenlabs/creatify-aurora`.
- **"avatar plus audio upload ... exceeds the gateway's 10 MiB request limit"** - use a shorter clip or an MP3 audio.
  See [Upload limit](#upload-limit).
- **"video generation with ... failed: \<provider message\>"** - the provider rejected the job: an unsupported `size`, an unreadable
  portrait or a moderation refusal. Adjust the request and retry.
- **"did not complete in time"** - renders can take a few minutes; raise `text_to_video.timeout`.
- **No partial files** - a failed render, timeout or download error never leaves a half-written clip behind; the output directory
  keeps only completed clips.
