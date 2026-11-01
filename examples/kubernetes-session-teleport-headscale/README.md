# Session teleport over Headscale: infer (local) → infer (k8s) → gateway

Teleport a session from an `infer daemon` on your machine to one in Kubernetes, whose agent then calls a gateway, with
every hop on a self-hosted [Headscale](https://github.com/juanfont/headscale) tailnet, admitted by the tailnet policy and
running mutual TLS. Then watch an attacker fail to read or reach any of it. This is the twin of
[kubernetes-session-teleport-netbird](../kubernetes-session-teleport-netbird/): the same chain and the same checks, with
Headscale and [Tailscale](https://tailscale.com/) clients in place of NetBird. With `TAILNET=tailscale` it runs
[on your Tailscale account](#run-it-on-your-tailscale-account) instead, with your own machine as `infer-local`.

```text
infer-local    Docker container beside the cluster, "your machine"
    │          ACL tag:infer-local → tag:infer-k8s:52789, mTLS with infer-local's client certificate
    ▼
infer-k8s      pod on agent-1, binding only on its tailnet address
    │          ACL tag:infer-k8s → tag:gateway:8443, mTLS with infer-k8s's client certificate
    ▼
gateway        pod on agent-0 with a tailscale sidecar, one of:
               - tokenless, a mock gateway, so no API key is needed (task up)
               - the Inference Gateway in front of a real provider (task gateway:real)
```

`infer-local` registers `infer-k8s` as a `kind: daemon` workspace by its MagicDNS name. A session starts on
`infer-local`, where turns are answered by the in-process mock model. After the teleport, the turns run on `infer-k8s`,
whose agent calls `gateway.tailnet.internal:8443`. The reply proves which path it took:
*Hello from tokenless, the mock gateway, reached over the tailnet.* With [a real model](#use-a-real-model), the model
answers instead.

Everything runs in a local [k3d](https://k3d.io/) cluster on k3s v1.37.1, declared in `Cluster.yaml` and created by
[ctlptl](https://github.com/tilt-dev/ctlptl), plus two Docker containers next to it for `infer-local`:

- Headscale v0.29.7, the control server, with its embedded DERP relay and STUN, so no traffic uses Tailscale's public
  relays
- the Tailscale client v1.104.4 as a sidecar in each tailnet pod, and beside the cluster for `infer-local`
- [cert-manager](https://cert-manager.io/) v1.21.3. It issues every certificate, Headscale's included, from one demo CA
- [tokenless](https://github.com/inference-gateway/tokenless) v0.11.0 as the gateway, a mock that answers like an
  Inference Gateway
- an attacker node and a packet sniffer

`task gateway:real` swaps tokenless for the [Inference Gateway](https://github.com/inference-gateway/inference-gateway)
0.61.1, run by the [Inference Gateway operator](https://github.com/inference-gateway/operator) v0.32.0 from a `Gateway`
resource, in front of a real provider.

The default run needs no account and no API key, `task gateway:real` needs your provider's key. See
[infer daemon](../../docs/daemon.md#daemons-on-other-machines) for the workspace model.

## Prerequisites

- [ctlptl](https://github.com/tilt-dev/ctlptl) and [k3d](https://k3d.io/) v5 (ctlptl drives k3d)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [docker](https://docs.docker.com/get-docker/) with Compose v2, on a kernel with `/dev/net/tun` (Docker Desktop and
  OrbStack have it)
- [task](https://taskfile.dev/) and Go 1.26: `task image:import` builds the infer image from this checkout
- `jq`, `openssl` and `column`

## Layout

The `headscale/` files are what Headscale needs. The `k8s/` and `local/` files follow the NetBird example's, with a
tailscale sidecar where NetBird's operator injected one.

| File | Purpose |
| --- | --- |
| `Cluster.yaml` | The ctlptl cluster: k3d `infer-headscale`, one server and two agents |
| `headscale/server.yaml` | Headscale in namespace `headscale`: its certificate, its Service (HTTPS 9443, STUN 3478, metrics 9090), its Deployment |
| `headscale/config.yaml` | Headscale's config: HTTPS, the embedded DERP and STUN, MagicDNS under `tailnet.internal`, the policy file |
| `headscale/policy.hujson` | The tailnet policy: tag owners and the two ACLs that chain `infer-local` to `infer-k8s` to the gateway |
| `tailscale/policy.hujson` | The same policy for your Tailscale account, with your own login in place of `tag:infer-local` |
| `k8s/namespace.yaml` | The `infer-peers` namespace, labelled so the Inference Gateway operator manages it |
| `k8s/certificates.yaml` | The demo CA `peers-ca`, a ClusterIssuer, and the `infer-k8s`, `gateway` and `infer-local` certificates, ECDSA P-256 |
| `k8s/infer-k8s.yaml` | The `infer-k8s` daemon and its tailscale sidecar: the binding on its tailnet address, the gateway as its model, both mTLS |
| `k8s/gateway.yaml` | tokenless with mTLS and its tailscale sidecar, and a NetworkPolicy that admits only WireGuard to the gateway's pods |
| `k8s/gateway-real.yaml` | The `Gateway` resource `task gateway:real` fills in with your provider, its tailscale sidecar in `spec.pod.containers` |
| `k8s/attacker.yaml` | A tailnet node that no ACL admits |
| `k8s/sniffer.yaml` | `tcpdump` on the host network of the `infer-k8s` node |
| `local/compose.yaml` | `infer-local`: the daemon and its tailscale client, two containers sharing one network namespace |
| `local/daemon.yaml` | `infer-local`'s config, with `infer-k8s` as a `kind: daemon` workspace |
| `Taskfile.yml` | Every step below |
| `.state/` | Created by the tasks: `infer-local`'s client certificate, the last session and capture, and infer-local's home on Tailscale |

The example builds infer from this checkout (`task image:import`), because `gateway.tls`, the client certificate
`infer-k8s` presents to the gateway, is newer than the last infer release.

## Quick start

```bash
# 1. Cluster, cert-manager and the certificates, Headscale and the pre-auth keys, the mock gateway, infer-k8s, the attacker, infer-local
task up

# 2. infer-k8s is reachable from infer-local by its MagicDNS name
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
infer-k8s    daemon  true       address=infer-k8s.tailnet.internal:52789 storage_id= version=... workspace=infer-k8s
```

`task demo` answers `say hello` on `infer-local` with the in-process mock, teleports the session, and sends one more
turn, which runs on `infer-k8s` and goes through the gateway:

```text
provisioning
checkpointing
restoring
ready
session now lives on infer-k8s in /home/infer/.infer/projects/project/<conversation id>
Hello from tokenless, the mock gateway, reached over the tailnet.
```

The `task up` steps, one by one:

| Task | What it does |
| --- | --- |
| `cluster:create` | The k3d cluster and the `infer-peers` namespace |
| `image:import` | The infer image from this checkout, imported into the cluster |
| `certs:install` | cert-manager |
| `secrets` | The `infer-k8s`, `gateway` and `infer-local` certificates, the demo CA as the ConfigMap `tailnet-ca`, and the demo secret |
| `headscale:install` | Headscale with its certificate, its config and its policy |
| `headscale:keys` | The user `infer` and one tagged, ephemeral, reusable pre-auth key per node, each in a Secret `tailnet-key-<node>` |
| `deploy` | The mock gateway, `infer-k8s` and the attacker, each with its tailscale sidecar |
| `local:up` | `infer-local`, once it reaches `infer-k8s.tailnet.internal:52789` |

## Use a real model

tokenless answers every turn with the same line. To have a real model answer, swap it for the Inference Gateway in
front of your provider, after `task up` and before `task demo`:

```bash
OPENAI_API_KEY=sk-... task gateway:real
task demo PROMPT="In one sentence, what is WireGuard?"
```

`PROVIDER` and `MODEL` pick another provider, with its key in `<PROVIDER>_API_KEY`:

```bash
OLLAMA_CLOUD_API_KEY=... task gateway:real PROVIDER=ollama_cloud MODEL=ollama_cloud/glm-5.3-flash
```

`task gateway:real`:

1. installs the Inference Gateway operator
2. stores the key in the Secret `gateway-provider`
3. removes tokenless (or the `Gateway` of an earlier run) and `infer-k8s`, then their offline nodes from Headscale, so
   the new pods get the plain names
4. creates the `Gateway` from `k8s/gateway-real.yaml` with the same tailscale sidecar in `spec.pod.containers`, the
   same `app: gateway` label, port 8443 and `gateway` certificate, requiring a client certificate through
   `spec.server.tls.clientCARef`
5. recreates `infer-k8s` with the model in the ConfigMap `infer-k8s-model`

The ACLs, the name `gateway.tailnet.internal` and the certificates stay as they are, so
`task attack` and `task sniff:report` work the same, except that the capture looks for tokenless's line. Recreating
`infer-k8s` drops the sessions that live there, so teleport them back first. The key reaches only the gateway's pod,
and the gateway reaches the provider over the provider's public HTTPS, outside the tailnet. `task cleanup` and
`task up` bring the mock back. Run `task gateway:real` again to switch to another provider or model.

## How the pieces fit

- **Joining the tailnet.** `infer-k8s`, the gateway and the attacker each carry a tailscale sidecar in kernel mode
  (privileged, for `/dev/net/tun`). Each logs in to `https://headscale.headscale.svc.cluster.local:9443` with its pre-auth key, and
  trusts Headscale's certificate through `SSL_CERT_FILE` pointing at the demo CA. Beside the cluster, `infer-local` runs
  the same client in its own container. It reaches Headscale through the k3d server node under the name the pods use.
- **Keys and names.** Each pre-auth key carries one tag, so the node it enrolls is owned by `tagged-devices` and named
  after its hostname (`infer-k8s`). The keys are ephemeral, so a node is removed once it goes offline, and reusable, so
  a recreated pod joins again with the same key. `task gateway:real` removes the old offline nodes right away, so the
  new ones get the plain names.
- **Resolving tailnet names.** The sidecar rewrites the pod's `resolv.conf` to Tailscale's resolver `100.100.100.100`,
  and all containers of a pod share that file. Beside the cluster, `infer-local` shares its client's network namespace,
  and with it the same file. Tailnet names resolve through MagicDNS under `tailnet.internal`. With
  `override_local_dns: false`, every other name goes to the resolver the pod had before, so cluster names keep working.
  A node only learns the names of the nodes the policy lets it reach or be reached by.
- **Access control.** Once a policy file is set, Headscale denies everything it does not accept. The two ACLs accept
  TCP 52789 from `tag:infer-local` to `tag:infer-k8s`, and TCP 8443 from `tag:infer-k8s` to `tag:gateway`, both one
  way. `infer-local` dials every connection to `infer-k8s`, the teleport back included, and `infer-k8s` dials the
  gateway.
- **Off the pod network.** `infer-k8s` waits until `tailscale0` has an address, then starts the daemon with
  `INFER_DAEMON_BINDING_HOST` set to it, so nothing listens on its pod address.
- **The gateway node.** Both gateways carry the same sidecar: tokenless in its Deployment, the Inference Gateway
  through the `Gateway`'s `spec.pod.containers` and `spec.pod.volumes`. It skips MagicDNS (`TS_ACCEPT_DNS=false`),
  since the gateway dials no tailnet name. The gateway listens on every address of its pod, so the NetworkPolicy
  `gateway-wireguard-only` admits only WireGuard (UDP 41641) to it, and port 8443 is reachable over the tailnet alone.
- **Relays.** Headscale's embedded DERP server and STUN are the only ones in the DERP map (`derp.urls: []`). The nodes
  connect directly where they can and fall back to that relay, which only ever sees WireGuard.
- **Mutual TLS on both hops.** See [TLS on every hop](#tls-on-every-hop). TLS runs inside the WireGuard tunnel, so
  everything is encrypted twice.

## Watch the chain

```bash
task tailnet:status
```

Headscale lists every node with its tag, and `infer-k8s` shows the two neighbours the policy lets it see, both
connected directly, with the bytes sent and received on each link:

```text
== Headscale: every node
NAME         TAGS             ADDRESS     STATE
infer-k8s    tag:infer-k8s    100.64.0.1  online
gateway      tag:gateway      100.64.0.2  online
attacker     tag:attacker     100.64.0.3  online
infer-local  tag:infer-local  100.64.0.4  online
== infer-k8s: the peers the policy lets it see, and how it reaches them
100.64.0.1  infer-k8s    tagged-devices  linux  -
100.64.0.2  gateway      tagged-devices  linux  active; direct 10.42.1.6:41641, tx 81444 rx 15372
100.64.0.4  infer-local  tagged-devices  linux  active; direct 172.18.0.2:41641, tx 22292 rx 22404
```

Headscale ships no web UI, and has no flow log of who connected to which port, so the capture below shows what crosses
the wire instead.

## What an attacker gets

**On the wire.** `sniffer` runs `tcpdump` on the host network of the `infer-k8s` node, so it sees both hops:
`infer-local` to `infer-k8s`, and `infer-k8s` to the gateway on the other agent. `task sniff:start` starts it and sends
that node a plaintext UDP canary from a throwaway container beside the cluster, to prove the capture reads plaintext
when there is any. `task sniff:report` reads the capture after `task demo`:

```text
WireGuard packets (udp port 41641):  498
binding packets (tcp port 52789):    0
gateway packets (tcp port 8443):     0
DEMO_SECRET in the capture:          0
the gateway's reply in the capture:  0
plaintext canary in the capture:     25
```

The teleport, the turn and the model's reply only exist inside WireGuard. The capture is saved to
`.state/teleport.pcap` for Wireshark. It leaves out the kubelet (10250) and API server (6443) ports, otherwise it
would also record the `kubectl exec` stream that reads it back.

**On the tailnet.** `attacker` is a node of the same tailnet, but no ACL admits its tag. `task attack` prints:

```text
== attacker: nslookup infer-k8s.tailnet.internal | tail -n 2
** server can't find infer-k8s.tailnet.internal: NXDOMAIN
== attacker: nc -z -v -w 3 100.64.0.1 52789
nc: connect to 100.64.0.1 port 52789 (tcp) timed out: Operation in progress
== attacker: nc -z -v -w 3 10.42.3.6 52789
nc: connect to 10.42.3.6 port 52789 (tcp) failed: Connection refused
== attacker: nslookup gateway.tailnet.internal | tail -n 2
** server can't find gateway.tailnet.internal: NXDOMAIN
== attacker: nc -z -v -w 3 100.64.0.2 8443
nc: connect to 100.64.0.2 port 8443 (tcp) timed out: Operation in progress
== attacker: nc -z -v -w 3 10.42.1.6 8443
nc: connect to 10.42.1.6 port 8443 (tcp) failed: Connection refused
== attacker: its tailnet peers
100.64.0.3  attacker  tagged-devices  linux  -
== infer-local: the gateway, two hops away
nc: bad address 'gateway.tailnet.internal'
gateway.tailnet.internal: unreachable from infer-local
== infer-local's network, the policy admits it, but no client certificate
curl: (56) OpenSSL SSL_read: OpenSSL/3.5.4: error:0A00045C:SSL routines::tlsv13 alert certificate required, errno 0
== infer on infer-k8s, the policy admits it, but without its client certificate
Gateway Status: Unreachable (failed to fetch models: Get "https://gateway.tailnet.internal:8443/v1/models?...": remote error: tls: certificate required)
```

Headscale never sends the attacker either node, so it can neither resolve their names nor reach their tailnet
addresses. On the pod network, `infer-k8s` has nothing listening and the NetworkPolicy refuses the gateway. Even
`infer-local` cannot skip a hop: no ACL lets it reach the gateway, so it never learns its name. And the network alone
is not enough on either hop: a client in `infer-local`'s own network namespace, which the policy admits, fails the TLS
handshake with `certificate required` unless it presents `infer-local`'s client certificate, and so does infer on
`infer-k8s` once its own client certificate is unset.

## TLS on every hop

cert-manager's `peers-ca` issues every certificate, and every server asks for a client certificate from that same CA:

- **`infer-local` → `infer-k8s`.** The binding serves `binding.tls.mode: files` with `client_auth: true` and the
  `infer-k8s` certificate (`infer-k8s.tailnet.internal`, `infer-k8s`, `localhost`). `infer-local` pins the CA
  (`tls.ca_file`) and presents its `infer-local` certificate (`tls.cert_file`, `tls.key_file`).
- **`infer-k8s` → `gateway`.** tokenless with `--tls-client-ca`, or the `Gateway` with `spec.server.tls.clientCARef`,
  serves the `gateway` certificate (`gateway.tailnet.internal`). infer adds the CA to its roots
  (`INFER_GATEWAY_TLS_CA_FILE`) and presents the `infer-k8s` certificate (`INFER_GATEWAY_TLS_CERT_FILE`,
  `INFER_GATEWAY_TLS_KEY_FILE`).
- **`gateway` → provider**, with `task gateway:real` only. The provider's public HTTPS, with your API key.
- **The infer commands → `infer-local`'s daemon.** `infer daemon workspaces`, `teleport` and `run` reach their own
  daemon on loopback. That binding serves `binding.tls.mode: ca`: the daemon makes its own CA, and the commands on the
  same machine present a client certificate from it. Loopback alone would let any local process or web page drive the
  daemon.
- **Every tailscale client → Headscale.** Headscale serves the `headscale-tls` certificate from `peers-ca` for its
  control API and its DERP relay. The clients trust that CA through `SSL_CERT_FILE`, and the control protocol is
  encrypted end to end on top of it.

The binding accepts `infer-local`'s verified certificate in place of a token, so there is none. `infer-k8s` holds one
certificate with both server and client usage, so it is one identity whether it serves `infer-local` or calls the
gateway. In infer, mutual TLS to the gateway is optional: `gateway.tls.ca_file` alone gives plain TLS against a private
CA, and the client certificate is presented only when the gateway asks for one.

## forward_env, sealed end to end

`local/daemon.yaml` lists `DEMO_SECRET` under `forward_env`. The teleport seals it with HPKE to the public key of the
certificate `infer-k8s` presents in that TLS handshake, and only the `infer-k8s` daemon holds that key. Sealing needs an
ECDSA P-256 key, which is why `k8s/certificates.yaml` asks for one. `task secret:check` compares hashes:

```text
infer-local DEMO_SECRET sha256: 135ba7aca6fe53a9
infer-k8s worker 501 sha256:   135ba7aca6fe53a9
DEMO_SECRET in the infer-k8s pod spec: 0
```

The value reached the session's worker on `infer-k8s`. It is not in any Kubernetes object there or in the daemon's own
environment, and the capture never saw it.

## Why the daemon terminates TLS, not `tailscale serve`

`tailscale serve --https` publishes a loopback binding under the node's name with a certificate the control server
provisions. Headscale v0.29 cannot provision those certificates (juanfont/headscale#3554 adds it on its main branch and
needs an ACME DNS-01 provider for a public domain). Behind Serve, the daemon would also lose two things this example
shows: `forward_env`, because a teleport seals the variables to the certificate the peer presents and Serve holds that
key, and client certificates, because Serve terminates TLS before the daemon sees the handshake. So the binding serves
`binding.tls.mode: files` on its tailnet address with a certificate from your own CA.

`binding.tls.mode: ca` does not fit `infer-k8s` either: its certificate names only the workspace, the hostname and
`localhost`, and the binding then requires client certificates from that daemon's own CA, which nothing issues to a
second daemon. It does fit `infer-local`'s own binding, which only the infer commands on the same machine dial.

## Run it on your Tailscale account

`TAILNET=tailscale` runs the same chain on your own [Tailscale](https://tailscale.com/) tailnet instead of Headscale,
with this machine as `infer-local`, on the Tailscale app you already run. The cluster's nodes show up in your admin
console's Machines list, tagged and ephemeral, and `task cleanup` removes them again.

| | Headscale (default) | `TAILNET=tailscale` |
| --- | --- | --- |
| `infer-local` | A Docker container with its own tailscale client, `tag:infer-local` | This machine as your own user, through its Tailscale app |
| Control server and relays | Headscale in the cluster and its embedded DERP | Tailscale's control plane and its public DERP relays |
| Names | `<node>.tailnet.internal` | `<node>.<tailnet>.ts.net`, read from the Tailscale app |
| Keys | `headscale preauthkeys create` | Minted per node through the Tailscale API from your OAuth client |
| Policy | `headscale/policy.hujson`, applied by `task up` | `tailscale/policy.hujson`, merged into your policy file by you |

Set up your account once:

1. Connect the Tailscale app on this machine, with MagicDNS on.
2. Run `task tailscale:policy TAILNET=tailscale`. It prints `tailscale/policy.hujson` with your login as the source of
   the first ACL. Add its `tagOwners` and `acls` entries to your tailnet policy file in the admin console. The tasks
   never write your policy file.
3. Create an OAuth client in the admin console under **Settings → Trust credentials**, with the `auth_keys` and
   `devices:core` write scopes and the one tag `tag:infer-demo`. The policy makes that tag the owner of the three node
   tags, so the client can mint a key for each node with only that node's tag. A client holding the three node tags
   itself is refused one with a single tag.
4. Export it in the shell that runs the tasks:

   ```bash
   export TAILSCALE_OAUTH_CLIENT_ID=... TAILSCALE_OAUTH_CLIENT_SECRET=...
   ```

Then pass `TAILNET=tailscale` to the first `task up`. It records the choice in `.state/`, so the other tasks need no
flag:

```bash
task up TAILNET=tailscale
task workspaces
task sniff:start
task demo
task secret:check
task sniff:report
task attack
task teleport:back
task cleanup
```

What the tasks do differently:

- **Keys.** `task up` mints one auth key per cluster node through the Tailscale API: tagged with that node's tag,
  ephemeral, reusable and pre-approved. Each goes into the Secret `tailnet-key-<node>`, as with Headscale. Your OAuth
  client secret stays in your shell, and the cluster only gets those keys. First, `task up` removes demo nodes an
  earlier run left behind, and it stops if another device in your tailnet already holds `infer-k8s`, `gateway` or
  `attacker`.
- **infer-local.** It is `infer` built from this checkout, run with `HOME` set to `.state/infer-local`, so its daemon,
  its `daemon.yaml` and its sessions stay apart from your own `~/.infer`. `task local:up` writes that `daemon.yaml`
  with `infer-k8s` as a `kind: daemon` workspace under your tailnet's name, starts the daemon in the background, and
  waits until `infer-k8s` is reachable. Its
  binding listens on loopback port 52790, so it does not clash with a daemon of your own on 52789. `task local:down`
  stops it.
- **Old nodes.** `task gateway:real` and `task cleanup` remove the demo's old nodes through the API, and
  `task cleanup` also revokes the auth keys. They only touch ephemeral devices named `infer-k8s`, `gateway` or
  `attacker` that carry nothing but the demo's tags.

Your policy file decides what the attacker reaches. If it still has a rule that lets every device reach every other
(`"src": ["*"]`), the attacker and your other devices reach both ports over the tailnet. Then only mutual TLS turns
them away, and `task attack` shows the TLS refusals but connections where Headscale shows timeouts. Narrow that rule,
for example to `autogroup:member`, to get the Headscale result.

The binding keeps its own TLS, so mutual TLS and `forward_env` work the same. On Tailscale, `tailscale serve` is also
an option for a daemon that needs neither: keep the binding on loopback with a token, run
`tailscale serve --bg --https=443 http://127.0.0.1:52789` beside it, and register it as
`address: infer-k8s.<tailnet>.ts.net:443` with that `token` and no `tls`, as
[infer daemon](../../docs/daemon.md#daemons-on-other-machines) describes.

## Security notes

- The tailscale sidecars run privileged for `/dev/net/tun`.
- The pre-auth keys are reusable for 30 days, so a leaked one enrolls new nodes with its tag until it expires.
  `headscale preauthkeys expire` revokes one early.
- Headscale keeps its database and keys on an `emptyDir`, so a restarted Headscale forgets every node. Use a volume in
  production.
- Headscale's metrics port 9090 is plain HTTP on every node. Keep it off the load balancer in production.
- With `TAILNET=tailscale`, the OAuth client can mint keys for the demo's tags and remove devices from your tailnet.
  The tasks read it from your environment, pipe it into curl and keep the one-hour API token in a file only you can
  read, under `.state/`. Delete the client in the admin console once you are done with the example.
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
| A tailscale container logs `authkey already used` | The keys come from an older run. Delete the `tailnet-key-*` Secrets, run `task headscale:keys` |
| tailscale logs `certificate signed by unknown authority` | `SSL_CERT_FILE` misses the demo CA. `task secrets` creates the `tailnet-ca` ConfigMap |
| A tailscale client never gets an address | Your Docker VM lacks `/dev/net/tun`. Docker Desktop and OrbStack have it |
| `task local:up` cannot reach `infer-k8s` | Check `docker logs infer-local-tailscale`. On Tailscale, check your policy file for the first ACL |
| `infer-k8s` is `REACHABLE false` | Check `task tailnet:status`: both nodes online, and `infer-local` listed as a peer of `infer-k8s` |
| A node is named `infer-k8s-<suffix>` | An offline node still holds the name. Delete it with `headscale nodes delete`, then recreate the pod |
| `task demo` hangs before `provisioning` | The daemons run an older infer. Run `task image:import`, recreate the `infer-k8s` pod, `task local:up` |
| The turn on `infer-k8s` fails to reach the model | Read the logs of `deploy/gateway-mock` or `deploy/gateway`, its `tailscale` container first |
| `task gateway:real` stops with `Set OPENAI_API_KEY first` | Export your provider's key in the same shell, named after `PROVIDER` |
| No `gateway` Deployment after `task gateway:real` | The namespace lacks `inference-gateway.com/managed=true`, or the operator is down |
| `tls: certificate required` from infer on `infer-k8s` | `INFER_GATEWAY_TLS_CERT_FILE` or `_KEY_FILE` is unset, or not from `peers-ca` |
| Image pulls or `task image:import` time out on DNS | Docker's resolver is stuck. Restart Docker Desktop |
| `the Tailscale API refused ...` | The OAuth client is deleted, or its secret was copied wrong. Create a new one with both scopes |
| `requested tags [...] are invalid or not permitted` | The OAuth client's tag is not `tag:infer-demo`, or that tag owns no node tag |
| `rename or remove these devices in your tailnet first` | A device of yours holds a name the demo needs. Rename it in the admin console |
