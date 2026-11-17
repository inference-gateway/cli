# Sandbox policy

Watch the sandbox say no: a `Read` the policy blocks outright, a read whose
grant headless cannot approve, and an unattended `Bash` write the kernel
refuses. The example runs offline with no API key and no gateway: the model is
a [tokenless](https://github.com/inference-gateway/tokenless) mock scripted in
[`scenarios.yaml`](scenarios.yaml). You need [Task](https://taskfile.dev) to run
the scenarios.

infer reads its sandbox policy from `~/.infer/sandbox.yaml` and its bash
allow-list from `~/.infer/tools.yaml`, both from the home directory only (a
project copy is ignored). The `home` task seeds a demo HOME at `.home/` with its
own copies of both files and the fixture files the scenarios touch, and every
other task runs `infer` with `HOME` pointed at it.

## Layout

```text
sandbox/
├── Taskfile.yml          # one task per scenario, every infer runs with HOME=.home
├── sandbox.yaml          # the demo policy: filesystem.allowed, filesystem.denied, filesystem.enforce
├── tools.yaml            # the demo bash allow-list, per agent mode
├── scenarios.yaml        # what the mock model does, per prompt
└── .gitignore            # ignores the demo HOME and the files a run leaves behind
```

## The policy

[`sandbox.yaml`](sandbox.yaml) sets `filesystem.allowed`, `filesystem.denied`
and `filesystem.enforce`:

- `allowed` names what the file tools may read and write: the working directory
  (`.`), `/tmp`, and `/dev/null` on `access: read`.
- `denied` closes paths. `.git/`, `auth.yaml`, `*.env`, `id_rsa` and `*.key` are
  blocked outright, while `.infer/` and `private/` carry `on_violation: approval`,
  which turns a denial into a grant someone must approve.
- `enforce: true` lets the kernel confine an unattended Bash child to the
  writable paths.

A list present in the file replaces the matching default list, so every entry
the demo needs is spelled out. The demo deliberately trims the default
credential entries (`id_dsa`, `id_ecdsa`, `*.pem`, `.environment`) to keep the
tables readable, keep them when you adopt this into your own file.

## The allow-list

[`tools.yaml`](tools.yaml) sets `tools.bash.mode.{all,plan,standard,auto}.allow`.
The effective list for a mode is the `all` baseline unioned with that mode's own
bucket, matched whole against each command:

- `all` is trimmed to a few read-only commands (`ls`, `head`, `git status`, ...).
- `plan` and `standard` add nothing, so only the baseline runs unattended there.
- `auto` carries the `.*` sentinel: every command runs unattended without the
  clean-command guard, which makes the kernel the only boundary - exactly what
  the confinement scenario needs.
- `safety.approval_behaviour: prompt` delivers an approval request to the user.
  Headless has no listener, so a gated call is blocked instead.

## Run it

`task` lists the scenarios. The headless tasks keep only the `"role":"tool"` line
of the JSON output, which carries the tool's result.

| Task | What happens |
| --- | --- |
| `task policy` | Prints the three tables of `infer debug sandbox`, explained below |
| `task validate` | `infer tools validate "ls"` passes (on the baseline) and `infer tools validate "curl --version"` exits non-zero |
| `task validate:append` | The off-list command passes once `INFER_TOOLS_BASH_ALLOW_APPEND` adds it: it beats the `--tools-bash-allow-append` flag |
| `task read-secret` | The mock model reads `secret.env`, which the blocking `*.env` entry closes, so `Read` refuses |
| `task ask-private` | The mock model reads `private/notes.md`, an `on_violation: approval` entry, so headless blocks the grant nobody can approve |
| `task confinement` | In `--mode auto` a `touch` outside every writable path fails with the kernel's permission error, the one inside lands |
| `task all` | Runs the three headless scenarios |
| `task clean` | Removes the demo HOME and the files the scenarios created |

Each task builds this checkout first (`task build`, a no-op while it is up to
date) and runs `../../infer`. To use the `infer` on your `PATH` instead, pass it
in: `task policy INFER=infer`.

`task confinement` needs an OS with a kernel sandbox: Linux (Landlock) or macOS
(Seatbelt). Windows has none, so the write lands and the check fails.

### What to expect

`task validate`:

```text
Command is allowed: ls

   ERROR

  command not allowed: curl --version (standard mode).
```

`task read-secret`:

```text
{"content":"access to path '.../secret.env' is excluded for security","failed":true, "role":"tool", ..., "tool_name":"Read"}
```

`task ask-private`:

```text
{"content":"read to path '.../private/notes.md' is denied by the sandbox (rule 'private/')","failed":true, "role":"tool", ..., "tool_name":"Read"}
```

`task confinement`, the refused write, then the one inside the working
directory:

```text
{"content":"exit status 1: touch: cannot touch '/var/tmp/outside-demo.txt': Permission denied\nthe
sandbox confines writes to the writable paths of ~/.infer/sandbox.yaml (filesystem.allowed) and
refuses to read, rename or delete its denied files (filesystem.denied): work inside them, or ask
the user to approve the command or add the path","failed":true, "role":"tool", ..., "tool_name":"Bash"}
{"content":"{\"tool_name\":\"Bash\", ..., \"data\":{\"command\":\"touch inside-demo.txt\",\"exit_code\":0, ...}}",
"failed":false, "role":"tool", ..., "tool_name":"Bash"}
```

## The three tables of `task policy`

1. **Policy** - the entries of `~/.infer/sandbox.yaml` over the defaults and what
   each covers: an anchored path (`.`) resolves from the current directory, a
   relative pattern (`*.env`) matches any file at any depth, either case.
2. **Built-in carve-outs** - the directories infer needs itself (skills, plans,
   artifacts, memory) stay readable and writable although the config dirs ask,
   and the Go and Rust library caches stay read-only.
3. **Kernel confinement of unattended Bash** - the paths a confined child may
   write in each agent mode (plan swaps the working directory for the
   scratchpad, and build caches and temp dirs are always open), with everything else
   left read and execute. Linux confines with Landlock and macOS with Seatbelt.
   On Linux the kernel only grants, so reads keep the policy check above. On
   macOS the kernel also refuses to read, rename or delete the blocking denied
   entries.

Confinement applies to the commands the allow-list runs unattended: a command
somebody approved runs unconfined, which is what the approval is for.

## Without Task

The tasks set four environment variables and seed `.home/`. The same run by
hand:

```bash
export INFER_GATEWAY_MOCK=true
export INFER_GATEWAY_MOCK_SCENARIOS="$PWD/scenarios.yaml"
export INFER_AGENT_MODEL=mock/openai/gpt-4o

mkdir -p .home/.infer private
cp sandbox.yaml tools.yaml .home/.infer/
printf 'mocked-credential\n' > secret.env
printf 'an internal draft nobody approved publishing\n' > private/notes.md

HOME="$PWD/.home" infer debug sandbox --no-colors
HOME="$PWD/.home" infer tools validate "curl --version"
HOME="$PWD/.home" infer headless --no-save --mode auto "probe the sandbox writes"
```

## Use a real model

Drop `INFER_GATEWAY_MOCK`, `INFER_GATEWAY_MOCK_SCENARIOS` and
`INFER_AGENT_MODEL` to talk to a real model through the gateway. The policy and
the allow-list stay the same, so the same refusals apply to what the model asks
for. To keep the policy for every session, copy `sandbox.yaml` and `tools.yaml`
into your own `~/.infer/` and start from there.

## Related

- [Configuration Reference](../../docs/configuration-reference.md) - every `sandbox.yaml` and `tools.yaml` key
- [Tools Reference](../../docs/tools-reference.md) - how each tool consults the policy
- [sandbox internals](../../internal/sandbox/README.md) - the bounded context behind the checks
