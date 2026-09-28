# Frame Sources & Vision Annotation

[← Back to README](../README.md)

**What** - named frame sources that feed images to the agent, plus a pluggable annotator that turns each frame into text.
**Why** - cheap or text-only models (DeepSeek, small local models) cannot read images, and a
vision model plus coordinates is often more than the task needs.
**How** - configure a `vision.annotator` model and one or more `vision.sources`; the agent then
reads frames through `GetLatestFrame` and arbitrary images through `ImageDecode`.

Cheap or text-only models (DeepSeek, small local models) can still "see". Named **frame sources**
feed images to the agent - the built-in `screen` source (computer-use screenshot streaming) plus any
number of **directory sources** watching a folder for camera frames - and a pluggable **image
annotator** turns each frame into text: a scene summary and a numbered element list with bounding
boxes.

```yaml
# .infer/config.yaml
vision:
  annotator:
    enabled: true
    model: anthropic/claude-haiku-4-5-20251001 # any vision model served by your gateway
  sources:
    camera-front:
      type: directory
      path: .infer/frames/front # wherever your camera process writes frames
      retention: { max_files: 100, max_age: 24h }
```

The agent reads frames via `GetLatestFrame(source, format)` and arbitrary image files via
`ImageDecode(image, prompt)`. Any orchestrator or model that speaks chat completions can use these
tools - a vision model gets the image itself and a text-only model gets the annotation text: with an
annotator configured, `GetLatestFrame` defaults to `format: annotated` (text replaces the frame) and
`format: regular` returns the raw image; `ImageDecode` always attaches the image and adds the
annotation when an annotator is configured. Annotation is a side-call through the
gateway, so any vision model it serves works - including fully local ones via Ollama. See the
[configuration reference](configuration-reference.md#vision-settings) for all options.

## Related

- [Tools Reference](tools-reference.md#vision-tools) - `GetLatestFrame`, `ImageDecode`, and the image tools
- [Computer Use](computer-use.md) - the built-in `screen` frame source
- [Configuration Reference](configuration-reference.md#vision-settings) - sources, retention, and annotator settings
