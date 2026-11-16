# sandbox

**What** - the sandbox bounded context: the policy that decides which paths the file tools may read and write,
which bash commands run without a prompt, where a bash child may write once the kernel confines it, and how a
denied path turns into a grant the user approves.
**Why** - a security decision needs one chokepoint. Keeping it out of `config` leaves that package as schema
and path constants, and keeping runtime grants apart from the configured policy keeps the system prompt
byte-stable so the provider prompt cache stays warm.
**How** - `domain/` holds the policy value objects (`Allowed` with an `Access`, `Denied` with a `Violation`),
the denial contract (`DeniedError`, `ParseDenial`) and the `SandboxAccess` approval name. The root package
holds the process-wide `Granted` set and `GrantFor`, and applies the `filesystem` section of `sandbox.yaml` plus the carve-outs (skills,
plugins, runtime dirs, memory, Go and Rust library dirs) through `ValidateRead` and `ValidateWrite`, and resolves the
per-mode bash allow-list through `IsBashCommandAllowed`, and lists the paths a bash child may write through
`WritePaths`. `infrastructure/` persists an "always" grant into `~/.infer/sandbox.yaml`, the only policy file,
and builds the confined bash child (`Command`, started through `Child.Start`): Landlock on Linux, applied on the
thread that forks bash so only the child inherits it, Seatbelt on macOS through `/usr/bin/sandbox-exec`, plain
bash elsewhere.

## How it plugs in

- Read, Tree, Grep, Wait, ImageDecode, TextToSpeech and the file writer call `ValidateRead`. Write, Edit,
  MultiEdit and Delete call `ValidateWrite`, which also rejects custom-tool directories and the read-only
  library carve-out (Go and Rust).
- Bash, Wait, the approval policy and command hooks call `IsBashCommandAllowed` and surface
  `BashCommandRejectionHint` when a command is denied. The system prompt lists `BashAllowedCommands`.
- Bash spawns a command the allow-list runs unattended through `sandboxinfra.Command`. With
  `filesystem.enforce` (default on) the kernel lets the child execute anything and write only under
  `WritePaths`: the writable allowed entries and grants, the writable carve-outs, the temp dir, the Go, cargo
  and npm caches, and `/dev`. In plan mode the project gives way to the scratchpad. On macOS the kernel also
  refuses to read, rename or delete the files the blocking deny entries name (`auth.yaml`, `*.key`, ...), so a
  spelling the argv walker cannot predict still fails, through a symlink, a copy or a clone alike. Directory
  entries (`.git/`) and globs the profile cannot say exactly stay with the path check, since git must keep
  reading its own tree, and Linux has no read denial, since Landlock only grants. A tool that needs a denied
  file unattended, `ssh` with a denied `id_*` or a dev server loading `.env`, fails and the agent asks for
  approval. A refused write or read surfaces `DeniedHint` in the tool result. A command somebody approved
  runs as approved, unconfined, so the approval flow for a path outside the sandbox keeps its meaning.
- Decision order: denied entries first (a blocking one fails whatever its position, otherwise the first
  approval entry asks), then the built-in carve-outs, then the first matching allowed entry, then the user is
  asked. Denied entries match case-insensitively, since a case-insensitive filesystem opens `.ENV` as `.env`.
  An empty allowed list is no boundary. A grant only unlocks what approval could have, so a granted
  directory still respects denied.
- The default policy denies `.infer/` with `on_violation: approval`, so the config dirs, whose files can hold
  tokens, ask before every read and write. The carve-outs skip that entry, so skills, plans, plugins, memory
  and runtime output stay open.
- `sandbox.yaml` loads over the defaults, so a list the file leaves out keeps its default, and a file that
  fails to parse or validate stops the CLI from starting rather than falling back to a wider policy.
- The agent's tool loop recovers a `DeniedError` from the flattened tool result with `ParseDenial`, raises a
  synthetic `SandboxAccess` approval for `GrantFor` (the exact path when a denied entry matched, otherwise the
  directory around it, except that a handback inside a config dir grants the exact file), and on approval calls
  `Granted.Add`. An auto-accept answer also calls `PersistGrant` unless a denied entry matched, since denied
  wins over any allowed entry it could write. `PersistGrant` refuses grants inside the config dirs, puts a
  write grant first and a read grant last, so a read grant never shadows a write entry.
- `ValidateWrite` refuses `~/.infer/sandbox.yaml` whatever `denied` says, a project copy is never loaded, and `infer config set`
  cannot reach the policy keys, so the agent cannot widen its own sandbox through the tools it runs.
- Concurrency: tools run in parallel goroutines, so `Granted` is the only mutable sandbox state and sits
  behind an RWMutex. `List` returns a copy, so no check holds the lock while it walks the filesystem.
  `cfg.Tools.Sandbox` is never written after load and is read without a lock. `SaveSandbox` replaces the file
  through a rename, so a worker that starts while another persists a grant never reads a torn policy.

## Related

- [Configuration Reference](../../docs/configuration-reference.md)
- [Tools Reference](../../docs/tools-reference.md)
