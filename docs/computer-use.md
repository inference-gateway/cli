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
read on macOS and on Linux under X11 or GNOME Wayland (AT-SPI2, which needs `at-spi2-core` and a session
D-Bus). Other Wayland compositors and Windows fall back to screenshots. On GNOME Wayland
(`XDG_CURRENT_DESKTOP` names GNOME) the pointer and keyboard go through mutter's remote desktop session, and
screenshots and window positions come from the shell, which answers only in unsafe mode - the
[computer image](#the-computer-image) is the place for that, not your own desktop. Actions are governed by
`computer_use.enabled`, rate limits and `computer_use.approval`. The
[desktop app](https://github.com/inference-gateway/desktop) visualizes what the agent is doing
(monitor, screen overlay, approvals). For a sandboxed desktop to drive, see
[examples/computer-use](../examples/computer-use/). To give an agent a desktop of its own in a Kubernetes
pod, see the [desktop workspace](../examples/session-teleport/README.md#a-computer-of-its-own-the-desktop-workspace).

## The computer image

`ghcr.io/inference-gateway/computer`, released with the CLI under the same version tags, is an Ubuntu 26.04
desktop for an agent of its own: `infer daemon` on a headless GNOME Shell session (Wayland, 1280x800) in Ubuntu
mode, with the dock, the Yaru theme, Files, Terminal and Text Editor, plus `tmux` so an agent can run
interactive programs such as `infer chat` from its Bash tool. GTK apps expose their accessibility tree
there, so the agent reads and presses controls by label before it falls back to screenshots. A small shell
extension puts the shell in unsafe mode, which is what lets the daemon take screenshots and place windows, and a
stand-in `org.freedesktop.login1` answers the shell, as a pod runs no systemd. The container starts as root for
that and drops to the `infer` user before the desktop starts.

It ships no browser. A workspace installs what its agents need with
[`user_data`](daemon.md#workspaces-and-teleports), a cloud-init user-data script the pod runs as root when it
boots. Its output lands in `/var/log/cloud-init-output.log`. Nothing opens at boot - an app with a desktop entry
shows in the app grid, and `firefox-esr` is pinned to the dock once it is installed:

```yaml
# ~/.infer/daemon.yaml
workspaces:
  - name: ubuntu-desktop
    kind: kubernetes
    namespace: agents
    image: ghcr.io/inference-gateway/computer:latest
    env:
      INFER_COMPUTER_USE_ENABLED: "true"
    user_data: |
      #!/bin/sh
      set -e
      apt-get update
      apt-get install -y --no-install-recommends gimp
```

Without `user_data` the pod boots straight to the desktop. `task container:build:computer` builds
the image locally as `ghcr.io/inference-gateway/computer:local`.

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
