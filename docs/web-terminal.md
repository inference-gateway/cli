# Web Terminal

[← Back to README](../README.md)

**What** - the chat TUI in a browser tab, served by `infer daemon` over xterm.js.
**Why** - reach your sessions from a phone, a tablet or another machine without installing anything, and keep
them running when the tab closes.
**How** - `infer chat --web` starts the local daemon when needed and opens its terminal page. Every tab runs one
`infer chat --daemon` in a PTY on the daemon, which is the same client a terminal is.

```text
browser (xterm.js) ── /terminal/ws ──┐
                                     ├── infer daemon ── PTY: infer chat --daemon ── session worker,
desktop app, extension ── /ws ───────┘                                               or the workspace that owns the session
```

## Quick start

```bash
infer chat --web
```

The command prints `http://127.0.0.1:<port>/terminal/?token=<token>` and opens it. Click `+` for a tab, close
it when you like: the session keeps running in the daemon, and `infer chat --daemon --session-id <id>` reattaches
from a terminal. `/teleport <workspace>` moves a tab's session to another machine or a Kubernetes pod, see
[Workspaces and teleports](daemon.md#workspaces-and-teleports). Everything in
[the daemon chat](daemon.md#the-chat-tui-infer-chat---daemon) applies to a tab.

## Live screen

With [Computer Use](computer-use.md) streaming on (`computer_use.screenshot.streaming_enabled`), the
**Screen** button shows what the tab's session sees: the daemon asks the session's worker for its latest frame
(`read_frame` in the [binding protocol](browser-extension-protocol.md#session-screen)) and serves it at
`/screen/?id=<session>`, which the page polls every two seconds. The frame comes from wherever the session runs,
so after `/teleport` to a desktop workspace the view shows that desktop, relayed through this daemon.
[examples/computer-use](../examples/computer-use/) runs it against a sandboxed desktop on a second daemon.

When the agent reaches a step a person has to do - a login, a two-factor code, a CAPTCHA, a cookie dialog
the judge keeps rejecting - press **Take over**: the image takes focus and clicks, wheel and keys inside it drive
the session's screen, with the poll dropping to 250 ms so typing feels live. The agent is held for the actions it
issues meanwhile and runs them after the hand-back; observations keep flowing. **Done**, or `Esc` twice, hands the
screen back and queues one user message, so the next turn starts from a fresh screenshot of what the person left
behind. The inputs the person types are theirs alone: they create no approval prompt, no judge call and no
conversation entry, and a take-over resolves the agent's open approval box for a `Computer` call as rejected.

## Where it is served

The page lives at `/terminal/` on the daemon's [binding](daemon.md#the-binding) listener, next to `/ws`,
`/artifacts/` and `/a2a`, so its host, port, token and TLS are the binding's (`~/.infer/daemon.yaml`, or the
`INFER_DAEMON_BINDING_*` overrides). The daemon logs the URL as `web terminal available` when it starts.

The page sends the token once, in the first frame of each tab's socket, and keeps it in the browser's session
storage after the first load, so a URL without it works in the same browser session. A socket without the token,
or without a verified client certificate when the binding requires them, is closed. A page served to a browser
that cannot present the client certificate `tls.mode: ca` requires shows the page but opens no tab.

## From other devices

A binding on any host but loopback must serve TLS. Point `binding.host` at `0.0.0.0` with `tls.mode: files` and a
certificate, then open the URL with this machine's address:

```yaml
# ~/.infer/daemon.yaml
binding:
  enabled: true
  host: 0.0.0.0
  port: 52789
  token: <shared secret>
  tls:
    mode: files
    cert_file: ~/.infer/tls/server.crt
    key_file: ~/.infer/tls/server.key
```

The token grants every holder a shell as your user wherever the session runs, so treat it like an SSH key: keep
the binding on loopback or a VPN interface, and never put it behind a proxy that strips TLS.
[examples/web-terminal](../examples/web-terminal/) runs both variants against a mock model.

## Related

- [infer daemon](daemon.md)
- [Daemon Binding Protocol](browser-extension-protocol.md)
- [Commands Reference](commands-reference.md)
