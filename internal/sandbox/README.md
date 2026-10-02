# sandbox

**What** - the sandbox bounded context: the policy that decides which paths the file tools may read and write,
which bash commands run without a prompt, and how a denied path turns into a grant the user approves.
**Why** - a security decision needs one chokepoint. Keeping it out of `config` leaves that package as schema
and path constants, and keeping runtime grants apart from the configured policy keeps the system prompt
byte-stable so the provider prompt cache stays warm.
**How** - `domain/` holds the denial contract (`DeniedError`, `DeniedPath`), the process-wide `Granted` set and
the `SandboxAccess` approval name. The root package applies the `sandbox.yaml` policy (`config.SandboxConfig`) plus the carve-outs
(skills, plugins, runtime dirs, memory, Go library dirs) through `ValidateRead` and `ValidateWrite`, and
resolves the per-mode bash allow-list through `IsBashCommandAllowed`. `infrastructure/` persists an
"always" grant into the userspace `sandbox.yaml`.

## How it plugs in

- Read, Tree, Grep, Wait, ImageDecode, TextToSpeech and the file writer call `ValidateRead`. Write, Edit,
  MultiEdit and Delete call `ValidateWrite`, which also rejects custom-tool directories and the read-only Go
  library carve-out.
- Bash, Wait, the approval policy and command hooks call `IsBashCommandAllowed` and surface
  `BashCommandRejectionHint` when a command is denied. The system prompt lists `BashAllowedCommands`.
- The agent's tool loop recovers a `DeniedError` from the flattened tool result with `DeniedPath`, raises a
  synthetic `SandboxAccess` approval for `GrantDir(path)`, and on approval calls `Granted.Add`. An
  auto-accept answer also calls `PersistDirectory`.
- `ValidateWrite` refuses both `sandbox.yaml` locations whatever `protected_paths` says, and `infer config set`
  cannot reach the policy keys, so the agent cannot widen its own sandbox through the tools it runs.
- Concurrency: tools run in parallel goroutines, so `Granted` is the only mutable sandbox state and sits
  behind an RWMutex. `List` returns a copy, so no check holds the lock while it walks the filesystem.
  `cfg.Tools.Sandbox` is never written after load and is read without a lock.

## Related

- [Configuration Reference](../../docs/configuration-reference.md)
- [Tools Reference](../../docs/tools-reference.md)
