# Computer use on a remote desktop, watched live

Two `infer daemon` containers stand in for your laptop and a remote Linux desktop. `laptop` serves the
[web terminal](../../docs/web-terminal.md) you open in your browser. `desktop` runs a headless desktop (Xvfb with
Chromium on it) with [Computer Use](../../docs/computer-use.md) enabled, registered on the laptop as a `kind: daemon`
workspace (see [daemons on other machines](../../docs/daemon.md#daemons-on-other-machines)). A tab's session
teleports to the desktop, the agent drives that screen, and the tab's **Screen** view shows it live, relayed
peer to peer through the laptop daemon.

```text
your browser ── https://localhost:52789/terminal/ ── laptop daemon ── relay ── desktop daemon ── session worker
                 Screen: GET /screen/?id=<session>        │            (agui, TLS)       │            Computer tool
                                                      read_frame ─────────────────► latest frame ◄── Xvfb :99 + Chromium
```

The model is a [tokenless](https://github.com/inference-gateway/tokenless) mock scripted in
[`desktop/scenarios.yaml`](desktop/scenarios.yaml), so everything runs offline with no API key. You need Docker
Compose. Until a release carries the live view, build the image from this checkout first and point the example at it:

```bash
task container:build                         # ghcr.io/inference-gateway/cli:local
export INFER_IMAGE=ghcr.io/inference-gateway/cli:local
```

## Run it

```bash
docker compose up --build
```

The desktop daemon generates a self-signed certificate the laptop pins, and the laptop seeds a small git project to
teleport. Open <https://localhost:52789/terminal/?token=computer-use-demo>, accept the laptop's self-signed
certificate, click `+` for a tab, then:

1. `/teleport desktop`. The status bar flips to `⇄ desktop` once the session lands there.
2. Click **Screen**. The view shows Chromium on the desktop container, refreshed every two seconds.
3. `browse to example.com`. The mock model takes a screenshot, clicks into the browser, presses `ctrl+l`, types the
   URL and presses `enter`, each a `Computer` call on the desktop, and the Screen view follows along.

Close the tab whenever you like, the session keeps running on the desktop. `infer chat --daemon --session-id <id>`
from a shell in the laptop container (`docker compose exec laptop sh`) reattaches to it.

With a real model, drop `INFER_GATEWAY_MOCK` and `INFER_GATEWAY_MOCK_SCENARIOS` from `docker-compose.yml`, point
`INFER_GATEWAY_URL` at a gateway on both daemons, and choose a vision model, since the agent then picks its steps
from its own screenshots.

## Approvals

Mouse and keyboard actions ask for approval in the tab that runs the turn, as `computer_use.approval` configures
([Configuration Reference](../../docs/configuration-reference.md#computer-use-computer_useyaml)). For a session
that runs unattended, start it in auto mode from the tab (`shift+tab`), inside this sandbox only.

## Cleanup

```bash
docker compose down -v
```

A desktop of its own in a Kubernetes pod is the
[session-teleport example](../session-teleport/README.md#a-computer-of-its-own-the-desktop-workspace).
