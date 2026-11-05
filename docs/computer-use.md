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
  streaming_enabled: true   # starts the screen frame source GetLatestFrame reads from
```

The display backend is detected automatically on macOS, Linux and Windows. The accessibility tree is
read on macOS and on Linux under X11 (AT-SPI2, which needs `at-spi2-core` and a session D-Bus). Wayland
and Windows fall back to screenshots. Actions are governed by
`computer_use.enabled`, rate limits and `computer_use.approval`. The
[desktop app](https://github.com/inference-gateway/desktop) visualizes what the agent is doing
(monitor, screen overlay, approvals). For a sandboxed desktop to drive, see
[examples/computer-use](../examples/computer-use/). To give an agent a desktop of its own in a Kubernetes
pod, see the [desktop workspace](../examples/session-teleport/README.md#a-computer-of-its-own-the-desktop-workspace).

## The person's take-over

A person can drive the same screen from the web terminal's Screen view without
going through the tool layer: the **Take over** button claims it, pointer,
wheel and keyboard on the image act on it directly, and **Done** (or `Esc`
twice) hands it back. While the person holds the screen the agent is held - its
input actions wait until the hand-back and its observations keep working - and
the run state tells every client who drives (`screenControl.held` on the run
stream). The take-over resolves an open approval box for a `Computer` call as
rejected and queues one continue message on hand-back, so the next turn starts
from a fresh screenshot; the person's inputs enter no transcript. See the
web terminal's [live screen](web-terminal.md#live-screen).

## Related

- [Tools Reference](tools-reference.md#accessibility-provider) - the computer-use tools and the accessibility provider
- [Configuration Reference](configuration-reference.md#computer-use-computer_useyaml) - approval and rate limits, then screen recording
- [Vision](vision.md) - how frames are annotated for text-only models
