# Session teleport over NetBird: infer (local) → infer (k8s) → gateway

Teleport a session from an `infer daemon` on your machine to one in Kubernetes, whose agent then calls a gateway, with
every hop on a [NetBird](https://netbird.io/) WireGuard network, admitted by its own NetBird policy and running mutual
TLS. Then watch an attacker fail to read or reach any of it.

```text
infer-local    Docker container beside the cluster, "your machine"
    │          policy infer-local-to-infer-k8s: TCP 52789, mTLS with infer-local's client certificate
    ▼
infer-k8s      pod on agent-1, binding only on its NetBird address
    │          policy infer-k8s-to-gateway: TCP 8443, mTLS with infer-k8s's client certificate
    ▼
gateway        pod on agent-0, only WireGuard on its pod address, one of:
               - tokenless, a mock gateway, so no API key is needed (task up)
               - the Inference Gateway in front of a real provider (task gateway:real)
```

`infer-local` registers `infer-k8s` as a `kind: daemon` workspace by its NetBird name. A session starts on
`infer-local`, where turns are answered by the in-process mock model. After the teleport, the turns run on `infer-k8s`,
whose agent calls `gateway.netbird.internal:8443`. The reply proves which path it took:
*Hello from tokenless, the mock gateway, reached over NetBird.* With
[a real model](#use-a-real-model), the model answers instead.

Everything runs in a local [k3d](https://k3d.io/) cluster on k3s v1.37.1, declared in `Cluster.yaml` and created by
[ctlptl](https://github.com/tilt-dev/ctlptl), plus two Docker containers next to it for `infer-local`:

- the self-hosted NetBird server: management, signal, relay, STUN and the embedded identity provider in the combined
  `netbirdio/netbird-server` v0.82.0, plus the dashboard v2.96.0
- the [NetBird Kubernetes operator](https://github.com/netbirdio/kubernetes-operator) v0.9.0. It turns the pods into
  NetBird peers by injecting a NetBird client sidecar
- [cert-manager](https://cert-manager.io/) v1.21.3. It serves the operator's webhook and issues every certificate
- [tokenless](https://github.com/inference-gateway/tokenless) v0.11.0 as the gateway, a mock that answers like an
  Inference Gateway
- an attacker peer and a packet sniffer

`task gateway:real` swaps tokenless for the [Inference Gateway](https://github.com/inference-gateway/inference-gateway)
0.61.0, run by the [Inference Gateway operator](https://github.com/inference-gateway/operator) v0.31.0 from a `Gateway`
resource, in front of a real provider.

The default run needs no account and no API key, `task gateway:real` needs your provider's key. See
[infer daemon](../../docs/daemon.md#daemons-on-other-machines) for the workspace model.

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
| `k8s/namespace.yaml` | The `infer-peers` namespace, labelled so the Inference Gateway operator manages it |
| `k8s/certificates.yaml` | The demo CA `peers-ca` and the `infer-k8s`, `gateway` and `infer-local` certificates, all ECDSA P-256 |
| `k8s/infer-k8s.yaml` | The `infer-k8s` daemon: binding on its NetBird address with mTLS, the gateway over mTLS as its model endpoint |
| `k8s/gateway.yaml` | tokenless with mTLS as the `gateway` peer, and a NetworkPolicy that admits only WireGuard to the gateway's pods |
| `k8s/gateway-real.yaml` | The `Gateway` resource `task gateway:real` fills in with your provider |
| `k8s/attacker.yaml` | A NetBird peer that no policy admits |
| `k8s/sniffer.yaml` | `tcpdump` on the host network of the `infer-k8s` node |
| `local/compose.yaml` | `infer-local`: the daemon and its NetBird client, two containers sharing one network namespace |
| `local/daemon.yaml` | `infer-local`'s config, with `infer-k8s` as a `kind: daemon` workspace |
| `Taskfile.yml` | Every step below |
| `.state/` | Created by the tasks: the NetBird API token, the owner's password, `infer-local`'s client certificate, the last session and capture |

The example builds infer from this checkout (`task image:import`), because `gateway.tls`, the client certificate
`infer-k8s` presents to the gateway, is newer than the last infer release.

## Quick start

```bash
# 1. Cluster, NetBird and its operator, peers and policies, certificates, the mock gateway, infer-k8s, the attacker, infer-local
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
Hello from tokenless, the mock gateway, reached over NetBird.
```

The `task up` steps, one by one:

| Task | What it does |
| --- | --- |
| `cluster:create` | The k3d cluster and the `infer-peers` namespace |
| `image:import` | The infer image from this checkout, imported into the cluster |
| `netbird:install` | The NetBird server and dashboard, with keys generated once |
| `netbird:bootstrap` | The owner and an API token (`POST /api/setup`), the `netbird.internal` DNS domain, the allow-all `Default` policy deleted |
| `netbird:operator` | cert-manager, then the NetBird operator with that token |
| `netbird:peers` | The groups, setup keys and sidecar profiles, then the two policies |
| `secrets` | The `infer-k8s`, `gateway` and `infer-local` certificates and the demo secret |
| `deploy` | The mock gateway, `infer-k8s` and the attacker |
| `local:up` | `infer-local`, once its NetBird client resolves `infer-k8s.netbird.internal` |

## Use a real model

tokenless answers every turn with the same line. To have a real model answer, swap it for the Inference Gateway in
front of your provider, after `task up` and before `task demo`:

```bash
OPENAI_API_KEY=sk-... task gateway:real
task demo PROMPT="what is WireGuard?"
```

`PROVIDER` and `MODEL` pick another provider, with its key in `<PROVIDER>_API_KEY`:

```bash
ANTHROPIC_API_KEY=sk-ant-... task gateway:real PROVIDER=anthropic MODEL=anthropic/<model>
```

`task gateway:real`:

1. installs the Inference Gateway operator
2. stores the key in the Secret `gateway-provider`
3. removes tokenless and its offline NetBird peer
4. creates the `Gateway` from `k8s/gateway-real.yaml` with the same `app: gateway` label, port 8443 and `gateway`
   certificate, requiring a client certificate through `spec.server.tls.clientCARef`, and presenting the `gateway`
   certificate to itself through `spec.client.tls`
5. recreates `infer-k8s` with the model in the ConfigMap `infer-k8s-model`

The NetBird policy, the name `gateway.netbird.internal` and the certificates stay as they are, so
`task attack` and `task sniff:report` work the same, except that the capture looks for tokenless's line. Recreating
`infer-k8s` drops the sessions that live there, so teleport them back first. The key reaches only the gateway's pod,
and the gateway reaches the provider over the provider's public HTTPS, outside WireGuard. `task cleanup` and
`task up` bring the mock back.

The Inference Gateway sends every provider call through its own `/proxy/<provider>` route at
`SERVER_HOST:SERVER_PORT`, here `https://0.0.0.0:8443`. With mutual TLS on its listener, that call has to verify the
gateway's own certificate and present a client certificate. So the `gateway` certificate also names the IP address
`0.0.0.0` and has client usage, and `spec.client.tls` trusts `peers-ca` and presents the `gateway` certificate. The gateway
adds that CA to the system roots, so the provider's public certificate still verifies. The gateway's own
[mTLS example](https://github.com/inference-gateway/inference-gateway/tree/main/examples/kubernetes/mtls) does the same.
[inference-gateway#822](https://github.com/inference-gateway/inference-gateway/issues/822) tracks dropping the need for
it.

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
  `infer-k8s-to-gateway` accepts TCP 8443, both one way. `infer-local` dials every connection to `infer-k8s`, the
  teleport back included, and `infer-k8s` dials the gateway.
- **Off the pod network.** `infer-k8s` waits until `wt0` has an address, then starts the daemon with
  `INFER_DAEMON_BINDING_HOST` set to it, so nothing listens on its pod address. The gateway listens on all addresses,
  and the Inference Gateway image has no shell to wait for `wt0`, so the NetworkPolicy `gateway-wireguard-only` admits
  nothing but WireGuard (UDP 51820) to the gateway's pod address.
- **The gateway peer.** Whichever gateway runs, its pod carries `app: gateway`, the label the NetBird `SidecarProfile`
  and the NetworkPolicy both select, runs on agent-0, away from `infer-k8s`, and serves the `gateway` certificate on 8443.
  The mock is the Deployment `gateway-mock`. The real one is the Deployment the Inference Gateway operator makes from the
  `Gateway` named `gateway`, pinned by `spec.pod.nodeSelector`. The peer keeps its pod's name
  (`gateway-mock-7ddf64d7c8-r976t`), and the extra DNS label keeps `gateway.netbird.internal` stable across rollouts and
  across the swap.
- **Mutual TLS on both hops.** See [TLS on every hop](#tls-on-every-hop). TLS runs inside the WireGuard tunnel, so
  everything is encrypted twice.

## Watch the chain in the NetBird dashboard

```bash
task dashboard
```

The task prints the dashboard URL and the owner's login: `admin@netbird.local` and the password in
`.state/owner-password`. Then look at:

- **Control Center → Group**, then pick `infer-k8s` with the selector (⇕) on the selected node. The Group view draws the
  policies the selected group is the source of on its right and the ones it is the destination of on its left, so the
  whole chain fits in one view: `infer-local` → `infer-local-to-infer-k8s` (TCP 52789) → `infer-k8s` →
  `infer-k8s-to-gateway` (TCP 8443) → `gateway`. The Control Center is an access explorer, not a network map, so it
  always starts from one selected node, the first alphabetically. Clicking any other group node opens its membership
  editor, so cancel it to leave the groups as they are.
- **Control Center → Peer**: only the policies the selected peer is the source of. `infer-local` shows the hop to
  `infer-k8s`, `infer-k8s` the hop to `gateway`, and `attacker`, where the view opens, has no edges at all.
- **Access Control → Policies**: `infer-local-to-infer-k8s` (TCP 52789) and `infer-k8s-to-gateway` (TCP 8443), and
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
gateway packets (tcp port 8443):     0
DEMO_SECRET in the capture:          0
the gateway's reply in the capture:  0
plaintext canary in the capture:     9
```

The teleport, the turn and the model's reply only exist inside WireGuard. The capture is saved to
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
== attacker: nc -z -v -w 3 100.120.180.188 8443
nc: connect to 100.120.180.188 port 8443 (tcp) failed: Host is unreachable
== attacker: nc -z -v -w 3 10.42.3.9 8443
nc: connect to 10.42.3.9 port 8443 (tcp) failed: Connection refused
== attacker: its NetBird peers
Peers count: 0/0 Connected
== infer-local: the gateway, two hops away
gateway.netbird.internal: unreachable from infer-local
== infer-local's network, the policy admits it, but no client certificate
curl: (56) OpenSSL SSL_read: OpenSSL/3.5.4: error:0A00045C:SSL routines::tlsv13 alert certificate required, errno 0
== infer on infer-k8s, the policy admits it, but without its client certificate
Gateway Status: Unreachable (failed to fetch models: Get "https://gateway.netbird.internal:8443/v1/models?...": remote error: tls: certificate required)
```

Management never sends the attacker either peer, so it can neither resolve their names nor reach their NetBird
addresses. On the pod network, `infer-k8s` has nothing listening and the NetworkPolicy refuses the gateway. Even
`infer-local` cannot skip a hop: no policy lets it reach the gateway, so it never learns its name. And the network
alone is not enough on either hop: a client in `infer-local`'s own network namespace, which the policy admits, fails the
TLS handshake with `certificate required` unless it presents `infer-local`'s client certificate, and so does infer on
`infer-k8s` once its own client certificate is unset.

## TLS on every hop

cert-manager's `peers-ca` issues every certificate, and every server asks for a client certificate from that same CA:

- **`infer-local` → `infer-k8s`.** The binding serves `binding.tls.mode: files` with `client_auth: true` and the
  `infer-k8s` certificate (`infer-k8s.netbird.internal`, `infer-k8s`, `localhost`). `infer-local` pins the CA
  (`tls.ca_file`) and presents its `infer-local` certificate (`tls.cert_file`, `tls.key_file`).
- **`infer-k8s` → `gateway`.** tokenless with `--tls-client-ca`, or the `Gateway` with `spec.server.tls.clientCARef`,
  serves the `gateway` certificate (`gateway.netbird.internal`). infer adds the CA to its roots
  (`INFER_GATEWAY_TLS_CA_FILE`) and presents the `infer-k8s` certificate (`INFER_GATEWAY_TLS_CERT_FILE`,
  `INFER_GATEWAY_TLS_KEY_FILE`).
- **`gateway` → provider**, with `task gateway:real` only. The gateway first calls its own `/proxy/<provider>` route
  on `0.0.0.0:8443` with the `gateway` certificate, then the provider's public HTTPS with your API key.

The binding accepts `infer-local`'s verified certificate in place of a token, so there is none. `infer-k8s` holds one
certificate with both server and client usage, so it is one identity whether it serves `infer-local` or calls the
gateway. In infer, mutual TLS to the gateway is optional: `gateway.tls.ca_file` alone gives plain TLS against a private
CA, and the client certificate is presented only when the gateway asks for one.

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
- tokenless has no container image, so its pod downloads the v0.11.0 release binary and checks it against the
  release's `checksums.txt` before it runs it.
- `task gateway:real` reads your provider key from the environment and pipes it into the Secret `gateway-provider`, so
  it never appears on a command line or in the task output. Only the gateway's pod gets it, as an environment variable.

## Cleanup

```bash
task cleanup
```

## Troubleshooting

| Symptom | Fix |
| --- | --- |
| `task netbird:bootstrap` fails on `POST /api/setup` | The account already exists but `.state/pat` is gone. Run `task cleanup` and `task up` again |
| Sign-in shows `Bad Request: Requested resource does not exist` | The page belongs to a cluster you recreated. Open <http://localhost:18081> again |
| The Control Center shows a single node and no edges | It opened on `attacker`. Use the Group tab and select `infer-k8s` |
| The operator logs `invalid header field value` | The API key Secret holds a newline. Run `task netbird:operator` again |
| A pod is rejected by `mpod-v1.netbird.io` | The operator is not ready yet. Wait for its pod in `netbird-operator`, then run `task deploy` |
| A NetBird client never gets an address | Your Docker VM's kernel lacks WireGuard. Docker Desktop and OrbStack have it |
| `infer-k8s` is `REACHABLE false` | Lazy connections open the tunnel on the first packet, so retry once. Then check `task netbird:status` |
| `task local:up` cannot resolve `infer-k8s.netbird.internal` | Check `docker logs infer-local-netbird` for the management connection |
| `ports are not available` on `task cluster:create` | Something else listens on 18080 or 18081. Free them, the config uses those numbers |
| `task demo` hangs before `provisioning` | The daemons run an older infer. Run `task image:import`, recreate the `infer-k8s` pod, `task local:up` |
| The turn on `infer-k8s` fails to reach the model | Read the logs of `deploy/gateway-mock`, or `deploy/gateway` after `task gateway:real` |
| `no route to host` after the gateway pod changed | The old peer keeps the `gateway` label until NetBird drops it. Retry, or delete it in Peers |
| `task gateway:real` stops with `Set OPENAI_API_KEY first` | Export your provider's key in the same shell, named after `PROVIDER` |
| No `gateway` Deployment after `task gateway:real` | The namespace lacks `inference-gateway.com/managed=true`, or the operator is down |
| `tls: certificate required` from infer on `infer-k8s` | `INFER_GATEWAY_TLS_CERT_FILE` or `_KEY_FILE` is unset, or not from `peers-ca` |
| `no models available` after `task gateway:real` | In the `deploy/gateway` logs, `401` is a wrong key, `0.0.0.0` an old certificate |
