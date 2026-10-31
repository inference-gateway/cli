# Session teleport over NetBird: infer (local) → infer (k8s) → gateway

Teleport a session from an `infer daemon` on your machine to one in Kubernetes, whose agent then calls an Inference
Gateway, with every hop on a [NetBird](https://netbird.io/) WireGuard network and admitted by its own NetBird policy.
Then watch an attacker fail to read or reach any of it.

```text
infer-local    Docker container beside the cluster, "your machine"
    │          policy infer-local-to-infer-k8s: TCP 52789 (teleport, relayed turns)
    ▼
infer-k8s      pod on agent-1, binding only on its NetBird address, files TLS and a token
    │          policy infer-k8s-to-gateway: TCP 8080 (model calls)
    ▼
gateway        pod on agent-0, the Inference Gateway, only WireGuard on its pod address
    │          cluster network
    ▼
tokenless      a mock OpenAI provider, so no API key is needed
```

`infer-local` registers `infer-k8s` as a `kind: daemon` workspace by its NetBird name. A session starts on
`infer-local`, where turns are answered by the in-process mock model. After the teleport, the turns run on `infer-k8s`,
whose agent calls `gateway.netbird.internal:8080`. The reply proves which path it took:
*Hello from the Inference Gateway in Kubernetes, reached over NetBird.*

Everything runs in a local [k3d](https://k3d.io/) cluster on k3s v1.37.1, declared in `Cluster.yaml` and created by
[ctlptl](https://github.com/tilt-dev/ctlptl), plus two Docker containers next to it for `infer-local`:

- the self-hosted NetBird server: management, signal, relay, STUN and the embedded identity provider in the combined
  `netbirdio/netbird-server` v0.82.0, plus the dashboard v2.96.0
- the [NetBird Kubernetes operator](https://github.com/netbirdio/kubernetes-operator) v0.9.0. It turns the pods into
  NetBird peers by injecting a NetBird client sidecar
- [cert-manager](https://cert-manager.io/) v1.21.3. It serves the operator's webhook and issues the `infer-k8s`
  certificate
- the [Inference Gateway](https://github.com/inference-gateway/inference-gateway) 0.60.1 in front of
  [tokenless](https://github.com/inference-gateway/tokenless) v0.10.0
- an attacker peer and a packet sniffer

Nothing needs an account or an API key. See [infer daemon](../../docs/daemon.md#daemons-on-other-machines) for the
workspace model.

## Prerequisites

- [ctlptl](https://github.com/tilt-dev/ctlptl) and [k3d](https://k3d.io/) v5 (ctlptl drives k3d)
- [kubectl](https://kubernetes.io/docs/tasks/tools/) and [helm](https://helm.sh/) v3.12 or later
- [docker](https://docs.docker.com/get-docker/) with Compose v2, on a kernel with WireGuard (Docker Desktop and OrbStack
  have it)
- [task](https://taskfile.dev/) and Go 1.26: `task image:import` builds the infer image from this checkout
- `curl`, `jq` and `openssl`
- ports 18080 (NetBird API) and 18081 (dashboard) free on localhost

## Layout

The `netbird/` files are what NetBird needs. The `k8s/` and `local/` files would stay almost the same over another
VPN.

| File | Purpose |
| --- | --- |
| `Cluster.yaml` | The ctlptl cluster: k3d `infer-netbird`, one server and two agents, ports 18080 and 18081 on its load balancer |
| `netbird/server.yaml` | The combined NetBird server in namespace `netbird`, its volume and its Service |
| `netbird/server-config.yaml` | The server's config. `task netbird:install` fills in fresh keys and stores it as a Secret |
| `netbird/dashboard.yaml` | The NetBird dashboard on <http://localhost:18081> |
| `netbird/operator-values.yaml` | The operator's Helm values: the self-hosted management URL, the client image, the one namespace it injects into |
| `netbird/peers.yaml` | A `Group` and `SetupKey` per peer, and a `SidecarProfile` for each pod: `infer-k8s`, `gateway`, `attacker` |
| `k8s/namespace.yaml` | The `infer-peers` namespace the pods run in |
| `k8s/certificates.yaml` | cert-manager issuers and the `infer-k8s` ECDSA P-256 certificate |
| `k8s/infer-k8s.yaml` | The `infer-k8s` daemon: binding on its NetBird address, `files` TLS, a token, the gateway as its model endpoint |
| `k8s/gateway.yaml` | The gateway, a NetworkPolicy that admits only WireGuard to it, and tokenless behind it |
| `k8s/attacker.yaml` | A NetBird peer that no policy admits |
| `k8s/sniffer.yaml` | `tcpdump` on the host network of the `infer-k8s` node |
| `local/compose.yaml` | `infer-local`: the daemon and its NetBird client, two containers sharing one network namespace |
| `local/daemon.yaml` | `infer-local`'s config, with `infer-k8s` as a `kind: daemon` workspace |
| `Taskfile.yml` | Every step below |
| `.state/` | Created by the tasks: the NetBird API token, the owner's password, the `infer-k8s` CA, the last session and capture |

## Quick start

```bash
# 1. Cluster, NetBird, operator, peers and policies, certificates, the gateway, infer-k8s, the attacker, infer-local
task up

# 2. infer-k8s is reachable from infer-local by its NetBird name
task workspaces

# 3. Start capturing in front of infer-k8s, then teleport a conversation and continue it through the gateway
task sniff:start
task demo

# 4. What reached infer-k8s, what crossed the wire, and what the attacker gets
task secret:check
task sniff:report
task attack

# 5. Bring the session home
task teleport:back
```

`task workspaces` prints:

```text
NAME         KIND    REACHABLE  DETAILS
infer-local  local   true       storage=jsonl version=...
infer-k8s    daemon  true       address=infer-k8s.netbird.internal:52789 storage_id= version=... workspace=infer-k8s
```

`task demo` answers `say hello` on `infer-local` with the in-process mock, teleports the session, and sends one more
turn, which runs on `infer-k8s` and goes through the gateway:

```text
provisioning
checkpointing
restoring
ready
session now lives on infer-k8s in /home/infer/.infer/projects/project/<conversation id>
Hello from the Inference Gateway in Kubernetes, reached over NetBird.
```

The `task up` steps, one by one:

| Task | What it does |
| --- | --- |
| `cluster:create` | The k3d cluster and the `infer-peers` namespace |
| `image:import` | The infer image from this checkout, imported into the cluster |
| `netbird:install` | The NetBird server and dashboard, with keys generated once |
| `netbird:bootstrap` | The owner and an API token (`POST /api/setup`), the `netbird.internal` DNS domain, the allow-all `Default` policy deleted |
| `netbird:operator` | cert-manager, then the operator with that token |
| `netbird:peers` | The groups, setup keys and sidecar profiles, then the two policies |
| `secrets` | The `infer-k8s` certificate, the binding token and the demo secret |
| `deploy` | tokenless, the gateway, `infer-k8s` and the attacker |
| `local:up` | `infer-local`, once its NetBird client resolves `infer-k8s.netbird.internal` |

## How the pieces fit

- **Joining NetBird.** In the cluster, each pod carries an `app` label. The operator's webhook matches it to a
  `SidecarProfile` and injects a NetBird client as a native sidecar, enrolled with that peer's setup key. Beside the
  cluster, `infer-local` runs the same client in its own container, enrolled with the `infer-local` setup key. It
  reaches the NetBird server through the cluster's load balancer under the name the pods use.
- **Names.** Each setup key puts its peer in a group of the same name and gives it that name as an extra DNS label. The
  setup keys are ephemeral, so management removes a peer a few minutes after its pod or container is gone, and it
  names ephemeral peers with an IP suffix (`infer-k8s-104-36`). The extra label keeps `infer-k8s.netbird.internal` stable across
  restarts.
- **Resolving NetBird names.** The NetBird client rewrites its `resolv.conf`. In the pods, the operator keeps that file
  on a shared volume, and the `infer-k8s` and attacker containers mount it at `/etc/resolv.conf`. Beside the cluster,
  `infer-local` shares its client's network namespace, and with it the same file. NetBird names resolve through the
  NetBird resolver, everything else as before. A peer only learns the names of the peers a policy lets it reach.
- **Access control.** NetBird denies by default once `Default` is gone. `infer-local-to-infer-k8s` accepts TCP 52789,
  `infer-k8s-to-gateway` accepts TCP 8080, both one way. `infer-local` dials every connection to `infer-k8s`, the
  teleport back included, and `infer-k8s` dials the gateway.
- **Off the pod network.** `infer-k8s` waits until `wt0` has an address, then starts the daemon with
  `INFER_DAEMON_BINDING_HOST` set to it, so nothing listens on its pod address. The gateway image has no shell to do the
  same, so the NetworkPolicy `gateway-wireguard-only` admits nothing but WireGuard (UDP 51820) to its pod address.
- **TLS and the token.** `infer-k8s` serves `binding.tls.mode: files` with a certificate cert-manager issues for
  `infer-k8s.netbird.internal`, `infer-k8s` and `localhost`. `infer-local` pins its CA (`tls.ca_file`) and sends the
  token. TLS runs inside the WireGuard tunnel, so the session is encrypted twice.

## Watch the chain in the NetBird dashboard

```bash
task dashboard
```

The task prints the dashboard URL and the owner's login: `admin@netbird.local` and the password in
`.state/owner-password`. Then look at:

- **Control Center → Peer**: pick `infer-k8s` in the peer selector. The graph shows `infer-local` → `infer-k8s` →
  `gateway`, each edge labelled with its policy. The view opens on the first peer alphabetically, `attacker`, which has
  no edges at all.
- **Control Center → Group**: the same chain between the `infer-local`, `infer-k8s` and `gateway` groups, and
  `attacker` alone.
- **Access Control → Policies**: `infer-local-to-infer-k8s` (TCP 52789) and `infer-k8s-to-gateway` (TCP 8080), and
  nothing else.
- **Peers**: the four peers, online, each with its group and NetBird address.
- **Activity**: the setup keys used, the peers added, the policies created and the DNS domain changed.

`task netbird:status` shows the chain from the middle. `infer-k8s` sees both neighbours connected `P2P`, with the
last WireGuard handshake and the bytes received and sent on each link:

```text
 infer-local-178-48.netbird.internal:
  Connection type: P2P
  ICE candidate endpoints (Local/Remote): 10.42.2.8:51820/172.18.0.2:51820
  Transfer status (received/sent) 14.6 KiB/14.5 KiB
 gateway-180-188.netbird.internal:
  Connection type: P2P
  ICE candidate endpoints (Local/Remote): 10.42.2.8:51820/10.42.3.9:51820
  Transfer status (received/sent) 7.7 KiB/70.7 KiB
```

Per-connection traffic events (who connected to which port, accepted or dropped) are a NetBird Enterprise feature. The
community server has no flow log, so the capture below shows what crosses the wire instead.

## What an attacker gets

**On the wire.** `sniffer` runs `tcpdump` on the host network of the `infer-k8s` node, so it sees both hops:
`infer-local` to `infer-k8s`, and `infer-k8s` to the gateway on the other agent. `task sniff:start` starts it and sends
a plaintext canary from `infer-local` to that node, to prove the capture reads plaintext when there is any.
`task sniff:report` reads the capture after `task demo`:

```text
WireGuard packets (udp port 51820):  507
binding packets (tcp port 52789):    0
gateway packets (tcp port 8080):     0
DEMO_SECRET in the capture:          0
INFER_K8S_TOKEN in the capture:      0
the gateway's reply in the capture:  0
plaintext canary in the capture:     9
```

The teleport, the token, the turn and the model's reply only exist inside WireGuard. The capture is saved to
`.state/teleport.pcap` for Wireshark. It leaves out the kubelet (10250) and API server (6443) ports, otherwise it
would also record the `kubectl exec` stream that reads it back.

**On the overlay.** `attacker` is a NetBird peer in the same network, but no policy admits its group. `task attack`
prints:

```text
== attacker: nslookup infer-k8s.netbird.internal | tail -n 2
** server can't find infer-k8s.netbird.internal: NXDOMAIN
== attacker: nc -z -v -w 3 100.120.104.36 52789
nc: connect to 100.120.104.36 port 52789 (tcp) failed: Host is unreachable
== attacker: nc -z -v -w 3 10.42.2.8 52789
nc: connect to 10.42.2.8 port 52789 (tcp) failed: Connection refused
== attacker: nslookup gateway.netbird.internal | tail -n 2
** server can't find gateway.netbird.internal: NXDOMAIN
== attacker: nc -z -v -w 3 100.120.180.188 8080
nc: connect to 100.120.180.188 port 8080 (tcp) failed: Host is unreachable
== attacker: nc -z -v -w 3 10.42.3.9 8080
nc: connect to 10.42.3.9 port 8080 (tcp) failed: Connection refused
== attacker: its NetBird peers
Peers count: 0/0 Connected
== infer-local: the gateway, two hops away
gateway.netbird.internal: unreachable from infer-local
```

Management never sends the attacker either peer, so it can neither resolve their names nor reach their NetBird
addresses. On the pod network, `infer-k8s` has nothing listening and the NetworkPolicy refuses the gateway. Even
`infer-local` cannot skip a hop: no policy lets it reach the gateway, so it never learns its name.

## forward_env, sealed end to end

`local/daemon.yaml` lists `DEMO_SECRET` under `forward_env`. The teleport seals it with HPKE to the public key of the
certificate `infer-k8s` presents in that TLS handshake, and only the `infer-k8s` daemon holds that key. Sealing needs an
ECDSA P-256 key, which is why `k8s/certificates.yaml` asks for one. `task secret:check` compares hashes:

```text
infer-local DEMO_SECRET sha256: 1f9dfe2e411305c7
infer-k8s worker 221 sha256:   1f9dfe2e411305c7
DEMO_SECRET in the infer-k8s pod spec: 0
```

The value reached the session's worker on `infer-k8s`. It is not in any Kubernetes object there or in the daemon's own
environment, and the capture never saw it.

## Why `files` TLS and not `ca`

`binding.tls.mode: ca` issues the binding a certificate from the daemon's own CA, but that certificate names only the
workspace, the hostname and `localhost`, and the binding then requires client certificates from that same CA. Nothing
issues those to a second daemon, so `ca` mode does not fit two daemons on different machines. `files` with your own CA
does, and cert-manager is that CA here. A binding behind a TLS-terminating proxy such as Tailscale Serve cannot use
`forward_env` at all, because the proxy holds the key.

## Security notes

- The NetBird control plane speaks plain HTTP in this demo (`http://netbird.netbird.svc.cluster.local:18080`), and so do
  the API and the dashboard on localhost. Put the management server behind TLS in production, as the official NetBird
  setup does.
- The operator's webhook rejects new pods in `infer-peers` while the operator is down (`failurePolicy: Fail`). The
  Helm values limit it to that namespace.
- The NetBird API token in `.state/pat` is the owner's and expires after 30 days. The operator uses it to manage the
  groups and setup keys.
- tokenless has no container image, so its pod downloads the v0.10.0 release binary and checks it against the
  release's `checksums.txt` before it runs it.

## Cleanup

```bash
task cleanup
```

## Troubleshooting

| Symptom | Fix |
| --- | --- |
| `task netbird:bootstrap` fails on `POST /api/setup` | The account already exists but `.state/pat` is gone. Run `task cleanup` and `task up` again |
| Sign-in shows `Bad Request: Requested resource does not exist` | The page belongs to a cluster you recreated. Open <http://localhost:18081> again |
| The Control Center shows a single peer and no edges | It opened on `attacker`. Pick `infer-k8s` in the peer selector, or use the Group tab |
| The operator logs `invalid header field value` | The API key Secret holds a newline. Run `task netbird:operator` again |
| A pod is rejected by `mpod-v1.netbird.io` | The operator is not ready yet. Wait for its pod in `netbird-operator`, then run `task deploy` |
| A NetBird client never gets an address | Your Docker VM's kernel lacks WireGuard. Docker Desktop and OrbStack have it |
| `infer-k8s` is `REACHABLE false` | Lazy connections open the tunnel on the first packet, so retry once. Then check `task netbird:status` |
| `task local:up` cannot resolve `infer-k8s.netbird.internal` | Check `docker logs infer-local-netbird` for the management connection |
| `ports are not available` on `task cluster:create` | Something else listens on 18080 or 18081. Free them, the config uses those numbers |
| `task demo` hangs before `provisioning` | The daemons run an older infer. Run `task image:import`, recreate the `infer-k8s` pod, `task local:up` |
| The turn on `infer-k8s` fails to reach the model | Check `kubectl -n infer-peers logs deploy/tokenless` and the gateway's logs |
