# Frame Sources & Vision Annotation

[← Back to README](../README.md)

**What** - named frame sources that feed images to the agent, plus a pluggable annotator that turns each frame into text.
**Why** - cheap or text-only models (DeepSeek, small local models) cannot read images, and a
vision model plus coordinates is often more than the task needs.
**How** - set `vision.annotator.enabled: true` and pick the `vision.annotator.model`, then add one or
more `vision.sources`; the agent then reads frames through `GetLatestFrame` and arbitrary images
through `ImageDecode`.

The built-in `screen` source is computer-use screenshot streaming. **Directory sources** watch a folder
for camera frames. The annotator produces a scene summary plus a numbered element list with bounding
boxes, so a vision model gets the image and a text-only model gets the text. Annotation is a side-call
through the gateway, so any vision model it serves works, including fully local ones via Ollama.

## Related

- [Tools Reference](tools-reference.md#vision-tools) - `GetLatestFrame` and `ImageDecode`. The image tools are under [Media Tools](tools-reference.md#media-tools)
- [Computer Use](computer-use.md) - the built-in `screen` frame source
- [Configuration Reference](configuration-reference.md#vision-settings) - sources, retention, and annotator settings
