# Media tools - video

One headless run walks the pipeline [Text-to-Video](../../docs/text-to-video.md) describes: `TextToVideo` renders a
clip, `VideoToSFX` scores it for the sound it shows and `DubVideo` re-renders the same clip with that sound as its
soundtrack. `VideoToSFX` arrives with `video_to_sfx.enabled`, `DubVideo` needs the extra `video_to_sfx.dub` switch.
The avatar flow then builds a presenter from a photo and lip-syncs it over a spoken line.

The model is a [tokenless](https://github.com/inference-gateway/tokenless) mock scripted in
[`scenarios.yaml`](scenarios.yaml) that answers the gateway's Videos API (`POST /v1/videos` job lifecycle, canned MP4
from `/content`), the SFX endpoint and the speech endpoint with canned media, so the example runs offline with no API
key and no gateway. You need [Task](https://taskfile.dev) to run it.

## Run it

`task` lists the scenarios. The headless ones run `infer headless` against the mock model and keep only the
`"role":"tool"` line of their JSON output, which names the saved file.

| Task | What happens |
| --- | --- |
| `task produce` | One headless run: `TextToVideo` saves `clip.mp4`, `VideoToSFX` scores that clip for its soundtrack, `DubVideo` dubs it |
| `task avatar` | `infer avatars create presenter --from photo.png`, `infer avatars list`, then a lip-sync run over the spoken line |
| `task local-models` | `infer models search musetalk` and `infer models list` against the Hugging Face Hub (needs internet, no API key) |
| `task all` | Runs produce and avatar |
| `task clean` | Deletes the presenter avatar |

Clips land in the video media directory and the score in the sfx media directory (see
[Directory Structure](../../docs/directory-structure.md)) - the tool result names the exact path.

### What to expect

```text
{"content":"{\"tool_name\":\"TextToVideo\", \"success\":true, ... \"path\":\"<media root>/video/clip.mp4\"}", ...}
{"content":"{\"tool_name\":\"VideoToSFX\", \"success\":true, ... \"path\":\"<media root>/sfx/clip.wav\"}", ...}
{"content":"{\"tool_name\":\"DubVideo\", \"success\":true, ... \"path\":\"<media root>/video/dub-clip.mp4\"}", ...}
```

### The avatar flow

`infer avatars create` stores the photo in `~/.infer/avatars/presenter/`. The example passes `--angles ""` - the
default two extra angle views are generated through the gateway's image edit API, which the mock does not cover (real
gateway, `tools.image_edit` enabled with a model: drop the flag and the create builds them). `infer avatars list`
shows the folder. The lip-sync task then looks the avatar up by name, drives it with the `TextToSpeech` clip and
renders it with `text_to_video.avatar_model`. The library never overwrites, so re-running `task avatar` reports that
presenter exists and keeps going - `task clean` removes it.

## Local models

The avatar render can run on this machine instead of at a provider: download the weights with `infer models` and set
`text_to_video.avatar_model` to the `local/` id, the way [Text-to-Video](../../docs/text-to-video.md) describes for
`local/musetalk`:

```bash
task local-models
infer models download TMElyralab/MuseTalk musetalkV15/unet.pth musetalkV15/musetalk.json
export INFER_TEXT_TO_VIDEO_AVATAR_MODEL=local/musetalk
task avatar
```

## Without Task

The Taskfile only sets the environment. Export it yourself and run `infer` from this directory:

```bash
export INFER_GATEWAY_MOCK=true
export INFER_GATEWAY_MOCK_SCENARIOS="$PWD/scenarios.yaml"
export INFER_AGENT_MODEL=mock/openai/gpt-4o
export INFER_TEXT_TO_VIDEO_ENABLED=true
export INFER_TEXT_TO_SPEECH_ENABLED=true
export INFER_VIDEO_TO_SFX_ENABLED=true
export INFER_VIDEO_TO_SFX_DUB=true

infer headless --no-save "render a drone shot, score it and dub it" | grep '"role":"tool"'
infer avatars create presenter --from photo.png --angles ""
infer avatars list
```

## With a real model

Drop `INFER_GATEWAY_MOCK`, `INFER_GATEWAY_MOCK_SCENARIOS` and `INFER_AGENT_MODEL` to talk to a real gateway. The
switches stay. The gateway needs its Videos API (`VIDEOS_ENABLED=true`, for `TextToVideo` and `DubVideo`) and its
Audio API (`AUDIO_ENABLED=true`, for `VideoToSFX` scoring) - the CLI-started gateway gets both on its own while the
switches above are set. The provider key belongs to the gateway, never to the CLI:

| Config key | Default model | Provider key the gateway needs |
| --- | --- | --- |
| `text_to_video.model` | `elevenlabs/veo-3.1-fast-generate-001` | an ElevenLabs API key |
| `text_to_video.avatar_model` | `elevenlabs/creatify-aurora` | an ElevenLabs API key |
| `video_to_sfx.model` | `mirelo/sfx-2.0` | a Mirelo API key |

Swap a model with `INFER_TEXT_TO_VIDEO_MODEL`, `INFER_TEXT_TO_VIDEO_AVATAR_MODEL` or `INFER_VIDEO_TO_SFX_MODEL` -
any `provider/model` the gateway serves, or a `local/` id for a downloaded model (above). Renders and dubbing can
take minutes and upload the portrait and the clip to the provider - see
[Gateway requirements](../../docs/text-to-video.md#gateway-requirements).
