# Computer use on a sandboxed desktop

One container runs a headless Linux desktop (Xvfb with Chromium on it) and `infer daemon` beside it, with
[Computer Use](../../docs/computer-use.md) enabled. The daemon serves the
[web terminal](../../docs/web-terminal.md) on its binding, so every browser tab is an `infer chat --daemon` session
whose agent drives the container's screen, never yours.

The model is a [tokenless](https://github.com/inference-gateway/tokenless) mock scripted in
[`scenarios.yaml`](scenarios.yaml), so everything runs offline with no API key. You need Docker Compose.

```text
your browser ── https://localhost:52789/terminal/ ── infer daemon ── infer chat --daemon (one per tab)
                                                        │                    │
                                                   session worker ── Computer tool ── Xvfb :99 + Chromium
```

## Run it

```bash
docker compose up --build
```

Open <https://localhost:52789/terminal/?token=computer-use-demo>, accept the self-signed certificate the container
generated on first start, and click `+` for a tab. Then ask:

```text
browse to example.com
```

The mock model takes a screenshot, clicks into the browser, presses `ctrl+l`, types the URL and presses `enter`,
each a `Computer` call that runs in the container, and answers
`I opened https://example.com in the browser on this desktop.`

`INFER_DAEMON_BINDING_TOKEN` in a `.env` file replaces the demo token, and `INFER_IMAGE` picks the infer image the
desktop is built on. With a real model, drop `INFER_GATEWAY_MOCK` and `INFER_GATEWAY_MOCK_SCENARIOS` from
`docker-compose.yml`, point `INFER_GATEWAY_URL` at a gateway, and choose a vision model, since the agent then picks its
steps from its own screenshots.

## Approvals

Mouse and keyboard actions ask for approval in the tab that runs the turn, as `computer_use.approval` configures
([Configuration Reference](../../docs/configuration-reference.md#computer-use-computer_useyaml)). For a session
that runs unattended, start it in auto mode from the tab (`shift+tab`), inside this sandbox only.

## Cleanup

```bash
docker compose down -v
```

A desktop of its own in a Kubernetes pod, reached by teleporting a session there, is the
[session-teleport example](../session-teleport/README.md#a-computer-of-its-own-the-desktop-workspace).
