# Text-to-Video

The CLI can render a short video clip in two modes:

- **Text to video** - a text prompt in, an `.mp4` out: b-roll, establishing shots, simple animated scenes.
- **Lip-synced avatar** - a portrait plus an audio clip in, a talking clip out: the portrait is animated to match the voice. This is the face
  half of the desktop's Content project workflow; `TextToSpeech` is the voice half.

Generation goes through the gateway's unified Videos API (`POST /v1/videos`, `GET /v1/videos/{id}`, `GET /v1/videos/{id}/content`, gateway v0.54.0+).
The CLI holds no provider key; the gateway does, and it routes the job to the configured `provider/model`: `model` for prompt renders
(default `elevenlabs/veo-3.1-fast-generate-001`) and `avatar_model` for lip-synced renders (default `elevenlabs/creatify-aurora`).
Requests appear in gateway logs and `infer traces` like any other request.

The feature is **disabled by default**, and for more than the usual zero-prompt-token reason: avatar renders upload the user's face and voice to a
third-party provider. While `text_to_video.enabled` is false, the `TextToVideo` tool definition is not sent to the LLM at all.

## Enabling

Add a `text_to_video` section to `.infer/config.yaml` (or `~/.infer/config.yaml`):

```yaml
text_to_video:
  enabled: true            # feature flag (default: false) - tool absent from the LLM payload when false
  model: ""                # prompt renders; "" = elevenlabs/veo-3.1-fast-generate-001
  avatar_model: ""         # lip-synced avatar renders; "" = elevenlabs/creatify-aurora
  size: ""                 # optional widthxheight passthrough, e.g. 720x1280; empty = provider default
  output_dir: ""           # where generated mp4s go; empty = ~/.infer/tmp/video
  timeout: 900             # whole-render timeout (seconds): create, poll and download
  poll_interval: 5         # job status poll cadence (seconds)
  create_avatar: false     # also give the agent the CreateAvatar tool (see Avatar library)
  require_approval: false  # optional; unset = no approval, like the image tools
```

Every field can also be set via environment variables, e.g. `INFER_TEXT_TO_VIDEO_ENABLED=true`,
`INFER_TEXT_TO_VIDEO_MODEL=elevenlabs/bytedance-seedance-v2-fast`, `INFER_TEXT_TO_VIDEO_AVATAR_MODEL=elevenlabs/creatify-aurora`,
`INFER_TEXT_TO_VIDEO_TIMEOUT=600`. Leaving `require_approval` unset is
not the same as setting it to `false`: unset keeps the tool's own default (no approval), an explicit value pins the policy.

## Models

A render with `audio` is an avatar render and goes to `avatar_model`; every other render (a prompt, optionally with a portrait as the
first frame or a library avatar as reference images) goes to `model`. Swap either one independently for any video model the gateway
serves - for ElevenLabs that includes
`veo-3.1-generate-001`, `veo-3.1-fast-generate-001` and the `bytedance-seedance-v2*` family for prompts, and `creatify-aurora` for
avatars. Avatar models take exactly one image and an audio clip, so a prompt-only render sent to one fails - which is why the two are
separate settings.

Both must be of the form `provider/model`; config validation rejects a bare model name, and a negative `timeout` or `poll_interval`,
as soon as the feature is enabled.

## Size rules

`size` is a passthrough of the gateway's `widthxheight` form - the CLI neither maps nor validates resolutions. For ElevenLabs models
the gateway derives the resolution from the shorter side (480, 720 or 1080) and the aspect ratio from the reduced ratio (`16:9`, `9:16`,
`1:1`), and rejects anything else. Each model supports a subset: `creatify-aurora` renders 480p or 720p only, and avatar renders keep
the portrait's aspect ratio (the gateway sends only the resolution):

| `size` | Render |
| --- | --- |
| `1280x720` | landscape 16:9, 720p |
| `720x1280` | portrait 9:16, 720p |
| `480x480` | square 1:1, 480p |

A gateway that cannot map a size rejects the job and the tool surfaces the provider's error message.

## Gateway requirements

The Videos API is part of the gateway (v0.54.0+; library avatars as reference images need v0.55.0+) and is served only with
`VIDEOS_ENABLED=true`. A gateway the CLI starts itself gets
the flag automatically while `text_to_video.enabled` is true; a gateway that was already running without it must be restarted. The
gateway must also hold credentials for the provider behind the models (for the defaults, an ElevenLabs API key). If you point the CLI
at an externally managed gateway, make sure it is on v0.54.0 or later, runs with `VIDEOS_ENABLED=true` and holds the provider key.

A gateway without the endpoint, or a provider that rejects the request, makes the tool call fail with a one-line error naming the
configured model (`video creation with elevenlabs/creatify-aurora failed: ...`). The agent run still completes, and no partial file
is left behind.

## Using the agent tool

With `text_to_video.enabled` set, the agent gains a `TextToVideo` tool:

- **Text-only clip** - ask for "a slow drone shot over neon rooftops"; the model calls `TextToVideo` with a `prompt`, optionally
  `seconds` and `size`. `seconds` is a passthrough string (providers accept a limited set of values); the gateway rejects unsupported ones.
- **Lip-synced avatar** - give it a portrait (`avatar`: the name of an avatar in the [library](#avatar-library), or a bare
  `.png`/`.jpg`/`.jpeg`/`.webp` file name in the working directory) and a driving clip (`audio`, a bare `.wav` or `.mp3` name: working
  directory first, then the `TextToSpeech` output directory - so a just-generated `TextToSpeech` clip can be lip-synced in the next
  tool call). `prompt` then describes framing only; the dialogue comes from the audio. `seconds` is ignored for avatar renders.
- **Same person, new shot** - a library `avatar` without `audio` sends every image in the avatar's folder as `reference_images`, so
  `model` keeps that person consistent in a prompt-driven shot (e.g. "the presenter walks into frame and says ..."). The gateway
  checks the count per model: `veo-3.1-*` takes at most 3 (the default `create` builds exactly 3) and needs its default 8-second
  length, `bytedance-seedance-v2*` takes up to 9 and `-v2.5` up to 30; models without reference-image support reject the render.
- **First frame** - a bare image file as `avatar` without `audio` renders the prompt with that portrait as the opening frame. The
  gateway does not combine a first frame with reference images.
- **Where files go** - `output_path` chooses the destination as a bare file name inside `output_dir` (default `~/.infer/tmp/video/`);
  otherwise a timestamped `video-*.mp4` is written there. The result reports the path, model, size and which avatar/audio were used.

The clip is always written as MP4 (`video/mp4` for creatify-aurora). To place it elsewhere, compose first and copy the returned file.

## Avatar library

An avatar is a folder under `~/.infer/avatars/<name>/` holding one or more portrait images (`.png`, `.jpg`, `.jpeg`, `.webp`) of the
same person - several shots from different angles are fine. Lip-sync models take a single image, so an avatar render uses the first
image in sort order (name it e.g. `01-front.png`); a prompt render sends all of them as reference images. The library lives beside
the config, not under `tmp/`, so `/reset` keeps it.

```text
~/.infer/avatars/
  presenter/
    01-front.png   # the image lip-sync models receive
    02-left.jpg    # prompt renders send every image as a reference
  host/
    portrait.webp
```

Manage it with `infer avatars`:

```bash
infer avatars create presenter --from ~/Pictures/me.jpg   # front photo + two generated three-quarter views
infer avatars list                                        # table of avatars and their images
infer avatars list --format json                          # [{"name":"presenter","images":["01-front.jpg", ...]}, ...]
infer avatars delete presenter                            # removes the folder and every image in it
```

`create` stores the photo as `01-front.<ext>` (the primary image) and generates each extra view from it through the gateway's
image edit API (`POST /v1/images/edits`) with `tools.image_edit.model` (default `openai/gpt-image-2`), all views in parallel. It
never overwrites an existing avatar, and a failed view removes the half-built folder. A JPEG is turned upright per its EXIF
orientation (phone photos are often stored sideways) and re-encoded, which strips its metadata - camera details and GPS location
never reach the library or a provider. PNG and WebP photos are stored as-is.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--from` | required | Front-facing photo (`.png`, `.jpg`, `.jpeg`, `.webp`) |
| `--angles` | `three-quarter-left,three-quarter-right` | Views to generate: also `left-profile`, `right-profile`; `""` only copies the photo |
| `--quality` | `high` | `auto`, `low`, `medium`, `high` or `standard` |
| `--size` | `1024x1536` | Generated image size as `WIDTHxHEIGHT` (portrait by default) or `auto`; `gpt-image-2` accepts arbitrary sizes |

Generating views sends the photo to the provider behind `tools.image_edit.model` (OpenAI by default), in addition to the video
provider at render time. The prompts ask for the same identity, clothing, lighting and background with a neutral, closed mouth,
but check the results - profiles drift more than three-quarter views. Lip-sync models only use the primary image; the extra
views feed prompt renders as reference images. Keep Veo's limit of 3 images in mind before adding profiles.

### CreateAvatar tool

With `text_to_video.create_avatar: true` (on top of `text_to_video.enabled`) the agent gets a `CreateAvatar` tool that runs the
same code as `infer avatars create`. It covers the flows the command cannot: an avatar from a portrait the agent just generated
with `ImageGeneration`/`ImageEdit`, a selfie sent over a channel such as Telegram, or a Content project building its presenter.

- **Opt-in, approval by default** - it is a separate switch because the agent could otherwise turn any face it sees into an
  avatar and send it to the image-edit provider. Every call asks for approval unless `text_to_video.require_approval` is set
  explicitly (`INFER_TEXT_TO_VIDEO_REQUIRE_APPROVAL`).
- **Photo confinement** - `photo` is a bare `.png`/`.jpg`/`.jpeg`/`.webp` file name looked up in the working directory (inside the
  sandbox), then in the session's artifacts directory where generated images land. It never reads absolute paths, `..` or
  anywhere else.
- **Create only** - an existing name fails, and there is no delete tool; deleting stays `infer avatars delete`.
- **Same knobs** - `angles` (default both three-quarter views, `[]` stores the photo only), `quality` (default `high`) and `size`
  (default `1024x1536`); generating angles needs `tools.image_edit` enabled with a model.

The result lists the saved images so the agent can go straight to `TextToVideo` with `avatar` set to the new name.

You can also build an avatar by hand: create the folder and copy the images in. When the agent passes an unknown avatar name the
tool call fails with the list of available avatars, so it can pick an existing one.

The portrait is sent inline with every render; nothing is stored as an avatar or asset at the provider, so there is nothing remote to
clean up. The rendered clips may still be kept in the provider's generation history.

## Upload limit

The avatar images and the driving audio travel in one multipart request body, and the gateway caps request bodies at 10 MiB by
default. The tool checks the combined size before sending and rejects bigger uploads with a one-line error suggesting a shorter clip,
an MP3 or fewer avatar images. A `TextToSpeech` WAV at 24 kHz mono is roughly 3 MB per minute, so typical clips fit; for longer audio prefer MP3.

## Troubleshooting

- **"video creation with ... failed: 404"** - the gateway is not serving `/v1/videos`: it predates v0.54 or runs without
  `VIDEOS_ENABLED=true`. See [Gateway requirements](#gateway-requirements).
- **"invalid text_to_video.model"** / **"invalid text_to_video.avatar_model"** - set the model as `provider/model`, e.g.
  `elevenlabs/creatify-aurora`.
- **"avatar ... not found (available avatars: ...)"** - pass one of the listed names, or add the avatar folder.
  See [Avatar library](#avatar-library).
- **"avatar images plus audio upload ... exceeds the gateway's 10 MiB request limit"** - use a shorter clip, an MP3 audio or
  fewer avatar images. See [Upload limit](#upload-limit).
- **"model ... accepts at most N 'reference_images'"** / **"does not accept 'reference_images'"** - the avatar folder holds more
  images than `model` takes, or `model` has no reference-image support: trim the folder or switch `model`.
- **"video generation with ... failed: \<provider message\>"** - the provider rejected the job: an unsupported `size`, an unreadable
  portrait or a moderation refusal. Adjust the request and retry.
- **"did not complete in time"** - renders can take a few minutes; raise `text_to_video.timeout`.
- **No partial files** - a failed render, timeout or download error never leaves a half-written clip behind; the output directory
  keeps only completed clips.
