# Computer Use

[← Back to README](../README.md)

**What** - tools that let the agent inspect and control a desktop: read accessible controls,
press them by label, capture screenshots, and drive the mouse and keyboard.
**Why** - some tasks only exist in a GUI, and structured accessibility data is cheaper and
more precise than guessing coordinates from a screenshot.
**How** - enable `computer_use.yaml`, then the agent uses the `Computer` and `GetLatestFrame`
tools; prefer the `accessibility` action and fall back to `screenshot`.

When enabled, the agent can inspect and control the desktop - read accessible controls, press them by
label, capture screenshots, move and click the mouse, scroll, and type text or key combinations. The
display backend is detected automatically across **macOS**, **Linux**, and **Windows**.

Computer Use is **off by default**. Turn it on in `computer_use.yaml` (or with `INFER_COMPUTER_USE_ENABLED=true`):

```yaml
# .infer/computer_use.yaml
enabled: true
rate_limit:
  enabled: true
screenshot:
  streaming_enabled: true   # also registers the GetLatestFrame tool
```

Tools: `Computer` and `GetLatestFrame`. `Computer` is action-based. Its `accessibility` action is the
preferred first observation on macOS: it returns compact `{role,label,state,bbox}` text in the same
coordinate space as screenshots, without a screenshot or vision-model call. Its `press` action invokes
an element's accessibility action by exact label without moving the cursor. Use `screenshot` only when
the accessibility tree is unavailable, empty, or insufficient; the remaining actions control the
pointer and keyboard.

The macOS AX bridge is implemented in Go with PureGo and runs in a short-lived helper process, so a
native accessibility failure degrades to screenshot guidance without taking down the CLI. Linux AT-SPI
and Windows UIA providers are not implemented yet and report that fallback explicitly. Computer-use
actions are governed by `computer_use.enabled`, rate limits, and `computer_use.approval`. The
[desktop app](https://github.com/inference-gateway/desktop) visualizes what the agent is doing
(monitor, screen overlay, approvals). For a sandboxed desktop to drive, see
[examples/computer-use](../examples/computer-use/).

## Related

- [Tools Reference](tools-reference.md#accessibility-provider) - the `Computer`, `GetLatestFrame`, `RecordStart`, `RecordStop` tools
- [Configuration Reference](configuration-reference.md#screen-recording-computer_use_yaml) - rate limits, screen recording, and approval settings
- [Vision](vision.md) - how frames are annotated for text-only models
