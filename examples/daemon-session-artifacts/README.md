# Session artifacts between two daemons

Two `infer daemon` processes on one machine, `laptop` and `workstation`, stand in for your laptop and a
workstation on the LAN (`kind: daemon`, see [daemons on other machines](../../docs/daemon.md#daemons-on-other-machines)).
A chat through the laptop daemon teleports its session to the workstation, the agent there generates an image into
the session's artifacts dir on the workstation, and `/artifacts` in the same chat lists it and pulls it home through
the laptop daemon. No shared storage, no file copying by hand.

The model is a [tokenless](https://github.com/inference-gateway/tokenless) mock scripted in
[`scenarios.yaml`](scenarios.yaml), so everything runs offline with no API key. You need
[Task](https://taskfile.dev) and `openssl`. The workstation serves TLS, as every daemon-kind workspace must, from a
self-signed certificate `task home` generates, which the laptop's `daemon.yaml` pins with `tls.ca_file`.

## Layout

```text
daemon-session-artifacts/
├── Taskfile.yml             # the tasks, with the mock model's environment
├── scenarios.yaml           # what the mock model says
├── laptop/daemon.yaml       # workspace laptop, binding on 52791, knows workstation at 127.0.0.1:52792
├── workstation/daemon.yaml  # workspace workstation, binding on 52792 over TLS
└── .home/                   # task home: a HOME per daemon, the certificate and the project (ignored by git)
    ├── laptop/.infer/…       .home/laptop/project is the repository the session teleports
    ├── workstation/.infer/…
    └── tls/server.{crt,key}
```

Each daemon runs with its own `HOME`, so each keeps its own conversations, leases and
`~/.infer/projects/<slug>/artifacts/<session id>/`. The workstation restores the teleported project under its
`projects_root`.

## Run it

In three shells:

```bash
task daemon:workstation   # 1. the workstation daemon
task daemon:laptop        # 2. the laptop daemon
task chat                 # 3. a daemon chat in the demo project
```

Then, in the chat:

1. `/teleport workstation`. The status bar flips to `⇄ workstation` once the session lands there.
2. `generate an image of a lighthouse at dusk`. The turn runs on the workstation, which saves the PNG under its own
   artifacts dir for the session.
3. `/artifacts`, or select the `⬇ artifacts` indicator. The listing comes from the workstation, relayed by the laptop
   daemon, which is what `read_artifact` and `list_artifacts` do in the
   [Daemon Binding Protocol](../../docs/browser-extension-protocol.md#session-artifacts).
4. `enter` saves the selected file under the laptop's artifacts dir for the session and shows the path, `o` saves
   and opens it with the system viewer.

`task artifacts` shows what each daemon holds: the image on the workstation, and the copy on the laptop once you saved
it. `task workspaces` reports whether the laptop daemon reaches the workstation, and `task clean` removes both HOMEs.

A session that teleported to a Kubernetes workspace works the same way, see [session-teleport](../session-teleport/).
