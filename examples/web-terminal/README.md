# Web terminal

`infer chat --web` opens the chat TUI in the browser. The page is served by `infer daemon` on its
[binding](../../docs/daemon.md#the-binding), and every browser tab runs one `infer chat --daemon` session, so
tabs outlive the browser, reattach from a terminal with `infer chat --daemon --session-id <id>`, and `/teleport`
to any workspace the daemon knows. See [Web Terminal](../../docs/web-terminal.md).

The model is a [tokenless](https://github.com/inference-gateway/tokenless) mock scripted in
[`scenarios.yaml`](scenarios.yaml), so everything runs offline with no API key. You need
[Task](https://taskfile.dev), and `openssl` for the LAN variant.

## On this machine

```bash
task web
```

`infer chat --web` starts the daemon when nothing listens on the binding's port, prints
`http://127.0.0.1:<port>/terminal/?token=<token>` and opens it. Click `+` for a tab, then ask
`where am I running?` or `list this directory`.

## From other devices

A binding on any host but loopback must serve TLS, so this variant runs the daemon from a HOME of its own,
[`daemon.yaml`](daemon.yaml), listening on every interface over a self-signed certificate `task home` generates:

```bash
task web:lan
```

Open the printed URL on a phone or another laptop with this machine's address in place of `127.0.0.1`, and accept
the self-signed certificate once. The token in the URL is the binding's, the same one the desktop app and peer daemons
present, and the page keeps it in its session storage after the first load. `task clean` removes the HOME.
