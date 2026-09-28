# Computer Use

[← Back to README](../README.md)

**What** - tools that let the agent inspect and control a desktop: read accessible controls,
press them by label, capture screenshots, and drive the mouse and keyboard.
**Why** - some tasks only exist in a GUI, and structured accessibility data is cheaper and
more precise than guessing coordinates from a screenshot.
**How** - enable `computer_use.yaml`, then the agent uses the `Computer` and `GetLatestFrame`
tools; prefer the `accessibility` action and fall back to `screenshot`.

Computer Use is **off by default**. Turn it on in `computer_use.yaml` (or with `INFER_COMPUTER_USE_ENABLED=true`):

```yaml
# .infer/computer_use.yaml
enabled: true
rate_limit:
  enabled: true
screenshot:
  streaming_enabled: true   # also registers the GetLatestFrame tool
```

The display backend is detected automatically on macOS, Linux and Windows. The accessibility tree is
macOS-only for now, and other platforms fall back to screenshots. Actions are governed by
`computer_use.enabled`, rate limits and `computer_use.approval`. The
[desktop app](https://github.com/inference-gateway/desktop) visualizes what the agent is doing
(monitor, screen overlay, approvals). For a sandboxed desktop to drive, see
[examples/computer-use](../examples/computer-use/).

## Related

- [Tools Reference](tools-reference.md#accessibility-provider) - the computer-use tools and the accessibility provider
- [Configuration Reference](configuration-reference.md#screen-recording-computer_useyaml) - rate limits, screen recording, and approval settings
- [Vision](vision.md) - how frames are annotated for text-only models
