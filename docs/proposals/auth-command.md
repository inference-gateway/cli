# Proposal: `infer auth` — Encrypted Provider Credential Storage

[← Back to README](../../README.md)

> **Status:** Draft / design proposal — _not yet implemented_.
> **Type:** Feature request.
> **Scope:** `inference-gateway/cli` (with a docs follow-up for `inference-gateway/docs`).
> **Author:** Maintainer agent.
> **Created:** 2026-06-11.

This document plans a new `infer auth` command family that lets users register provider API keys through a guided
prompt and stores them **encrypted at rest** (OS keyring when available, an encrypted file with a per-file salt
otherwise) instead of a plaintext `.env` file. It surveys how other CLIs solved the same problem and proposes a
concrete, phased implementation for this repository.

## Table of Contents

- [Summary](#summary)
- [Motivation](#motivation)
- [Goals and Non-Goals](#goals-and-non-goals)
- [How It Works Today](#how-it-works-today)
- [Prior Art: How Other CLIs Store Credentials](#prior-art-how-other-clis-store-credentials)
- [Proposed Design](#proposed-design)
  - [Command Surface](#command-surface)
  - [Storage Backends](#storage-backends)
  - [Encrypted-File Format](#encrypted-file-format)
  - [Passphrase Handling](#passphrase-handling)
  - [Provider Registry (Single Source of Truth)](#provider-registry-single-source-of-truth)
  - [Credential Resolution at Gateway Start](#credential-resolution-at-gateway-start)
- [Implementation Plan](#implementation-plan)
- [Configuration Changes](#configuration-changes)
- [Security Considerations](#security-considerations)
- [Backward Compatibility and Migration](#backward-compatibility-and-migration)
- [Testing Strategy](#testing-strategy)
- [Cross-Repo Impact and Docs Follow-Up](#cross-repo-impact-and-docs-follow-up)
- [Dependencies](#dependencies)
- [Open Questions](#open-questions)
- [References](#references)

---

## Summary

Add an `infer auth` command group so a user can run:

```bash
infer auth login anthropic        # prompts for the key, stores it encrypted
infer auth status                 # shows which providers are configured + which backend holds them
infer auth logout openai          # removes a stored key
infer auth import --from-env      # migrates an existing .env into the encrypted store
```

Keys are stored **encrypted at rest**:

1. in the OS keyring (macOS Keychain, Windows Credential Manager, Linux Secret Service) when one is reachable, and
2. otherwise in an encrypted file under `~/.infer/` (userspace) or `.infer/` (project, git-ignored), using a
   passphrase-derived key (Argon2id + a per-file random **salt**) and authenticated encryption.

At runtime the gateway manager resolves provider keys from this store (falling back to environment variables and the
legacy `.env` for backward compatibility) instead of requiring a hand-edited `.env`.

## Motivation

Today, connecting the CLI to providers requires copying [`.env.example`](../../.env.example) to `.env` and pasting raw
API keys into it. That flow has real problems:

- **Plaintext secrets on disk.** `.env` holds long-lived provider keys in cleartext. Anything that can read the file
  (other processes, backups, sync clients, a shared dev box) reads the keys.
- **Easy to leak into git.** `.gitignore` ignores `**/.env*` (keeping `*.env*.example`), but that protection is one
  `git add -f`, one misconfigured tool, or one copied-into-another-repo away from committing live keys. There is no
  defense in depth.
- **Poor onboarding UX.** New users must know which env var name maps to which provider, find `.env.example`, copy it,
  and edit it by hand. There is no guided `infer auth login <provider>`.
- **No lifecycle management.** There is no supported way to list, rotate, or remove a single provider's key; you edit a
  file. There is no way to see _which_ providers are currently configured.
- **Drift between the example and the loader.** The provider list lives in three places that already disagree.
  `.env.example` lists `MINIMAX_API_KEY` and `MOONSHOT_API_KEY`, but the gateway container path in
  [`internal/services/gateway_manager.go`](../../internal/services/gateway_manager.go) (`runContainer`'s
  `apiKeyEnvVars` slice) does **not** forward them — so a MiniMax/Moonshot key in `.env` is silently dropped in
  container mode. A single source of truth for providers fixes this class of bug.

A first-class `infer auth` command with encrypted storage addresses all five.

## Goals and Non-Goals

**Goals:**

- A guided `infer auth login <provider>` that captures a key and persists it encrypted.
- Encryption at rest by default — **zero plaintext secrets on disk** in the happy path.
- Use the OS keyring when available; fall back to a passphrase-encrypted file with a per-file salt.
- A single provider registry reused by both `infer auth` and the gateway manager (kills the drift above).
- Backward compatibility: existing `.env` / `INFER_*` / shell-exported keys keep working unchanged.
- A migration path from `.env` to the encrypted store.
- Secure-by-default and **fail-closed**, consistent with the project's existing posture (see
  [CLAUDE.md](../../CLAUDE.md) on headless approval and bash allow-listing).

**Non-Goals (for the first iteration):**

- OAuth / browser-based device-code login flows (providers here use static API keys, not OAuth).
- A built-in secret-manager integration (Vault, 1Password, AWS Secrets Manager). An external
  credential-helper escape hatch is noted as a future extension.
- Encrypting the rest of `config.yaml`. Only secrets move into the encrypted store.
- Changing the gateway server itself. This is a CLI-side change; the gateway keeps reading the same env var names.

## How It Works Today

Provider keys reach the model through the **gateway**, which the CLI can run for the user
([CLAUDE.md → "Two runtime modes"](../../CLAUDE.md)). The relevant code is
[`internal/services/gateway_manager.go`](../../internal/services/gateway_manager.go):

- **Binary mode** (`runBinary` → `loadEnvironment`): reads `.env` from the working directory, parses it line by line
  (a small hand-rolled `KEY=VALUE` parser, not a dotenv library), and appends the pairs onto `os.Environ()` for the
  spawned gateway process.
- **Container mode** (`runContainer`): passes `--env-file .env` to `docker run` when `.env` exists, and _separately_
  forwards an explicit allow-list of provider variables it reads from the parent environment:

  ```go
  apiKeyEnvVars := []string{
      "ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GOOGLE_API_KEY", "DEEPSEEK_API_KEY",
      "GROQ_API_KEY", "MISTRAL_API_KEY", "CLOUDFLARE_API_KEY", "COHERE_API_KEY",
      "OLLAMA_API_KEY", "OLLAMA_CLOUD_API_KEY",
  }
  ```

  (Note: this slice is missing `MINIMAX_API_KEY` and `MOONSHOT_API_KEY`, which `.env.example` advertises — the drift
  noted above.)

Separately, the **web-search tool** reads `GOOGLE_SEARCH_API_KEY`, `GOOGLE_SEARCH_ENGINE_ID`, and
`DUCKDUCKGO_SEARCH_API_KEY` directly via `os.Getenv` in
[`internal/agent/tools/web_search.go`](../../internal/agent/tools/web_search.go). These are not provider model keys
but are part of the same `.env` today and should be representable in the same store.

So the surface area to integrate with is small and well-contained: **the credential resolver must feed
`gateway_manager.go` (both modes) and, optionally, the tool-level `os.Getenv` lookups.**

Configuration layering is already established in [`cmd/root.go`](../../cmd/root.go) `initConfig()`: built-in defaults →
`~/.infer/config.yaml` → `./.infer/config.yaml` → flags → `INFER_*` env vars (highest). The new store slots in as an
additional, secret-only source — see [Credential Resolution](#credential-resolution-at-gateway-start).

## Prior Art: How Other CLIs Store Credentials

A survey of established CLIs, with each claim traceable to official docs or source (see [References](#references)).

| Tool | Storage location | Encrypted at rest? | OS keyring? | Mechanism |
| --- | --- | --- | --- | --- |
| **GitHub CLI** | keyring; `hosts.yml` fallback | Keyring-protected; fallback plaintext | **Yes (default)** | `zalando/go-keyring` |
| **Stripe CLI** | keyring (live); `config.toml` (test) | Live via keyring; file fallback JWE | **Yes** | `99designs/keyring` |
| **Docker CLI** | `~/.docker/config.json` | **No** — `base64(user:pass)` only | Helper binaries only | `docker-credential-*` |
| **AWS CLI** | `~/.aws/credentials` (+ caches) | **No — plaintext INI** | No | none (the anti-pattern) |
| **flyctl** | `~/.fly/config.yml` | No — plaintext YAML | No | none |
| **Vercel CLI** | `auth.json` (+ keyring) | File plaintext; keyring OS-backed | **Optional** (`credStorage`) | `@napi-rs/keyring` |
| **Heroku CLI** | `~/.netrc` | No — plaintext netrc | No | `netrc-parser` |
| **npm** | `~/.npmrc` | No — plaintext (`_authToken`) | No | none |

**Lessons we adopt:**

- **Keyring-first, like `gh`.** Try the OS keyring on every read/write; it is the strongest at-rest protection and
  needs no passphrase. `gh` uses the small, pure-keychain `zalando/go-keyring` — a good fit for our "minimal new
  dependencies" goal.
- **Split by sensitivity, like Stripe.** Only secrets go to the secure store; non-secret metadata (which providers
  exist, which backend, last-updated) can live in normal config.
- **A backend selector, like Vercel's `credStorage`.** Expose `--storage auto|keyring|file|env` so users on headless
  boxes, WSL, or CI can choose deterministically.
- **An env-var escape hatch, like all of them.** Every surveyed tool lets an env var override stored creds. We already
  have `INFER_*`; keep it authoritative for CI so nothing has to touch disk.

**Anti-patterns we explicitly avoid:**

- **AWS-style plaintext keys** as the default path. Long-lived keys in a readable file is exactly the status quo we are
  replacing.
- **Docker-style base64** masquerading as protection. Base64 is encoding, not encryption.
- **`gh --insecure-storage` silent plaintext fallback.** When neither keyring nor an encrypted file is usable, we
  **fail closed** rather than quietly writing plaintext (consistent with this project's headless posture).

**Crypto baseline when no keyring is available** (current OWASP / RFC 9106 guidance):

- **KDF:** Argon2id (`golang.org/x/crypto/argon2.IDKey`). For a local, interactive single-secret unlock we can afford
  more memory than OWASP's server-side login minimums — target **64 MiB memory, time ≥ 3, threads ≈ CPU cores**, key
  length 32 bytes, with the parameters stored in the file header so they can be raised later.
- **Salt:** 16 cryptographically random bytes, **unique per file**, stored next to the ciphertext (a salt is not
  secret).
- **AEAD:** XChaCha20-Poly1305 (`golang.org/x/crypto/chacha20poly1305.NewX`, 24-byte random nonce — safe to randomize)
  or NaCl `secretbox`. Both are in the already-vendored `golang.org/x/crypto`.
- **Permissions:** file `0600`, directory `0700` (what `gh` and `99designs/keyring` do).

## Proposed Design

### Command Surface

Model the surface on `gh auth` (the closest proven analog):

```text
infer auth login <provider>     Add/replace a provider key (prompt by default)
    --with-token                Read the key from stdin (scripting / CI)
    --storage auto|keyring|file  Choose the backend (default: auto)
    --project                   Store in ./.infer instead of ~/.infer (file backend)
infer auth logout <provider>    Remove a stored key (keyring + file)
infer auth status               List configured providers + backend (never prints secrets)
infer auth list                 List known providers and their env-var names + configured state
infer auth token <provider>     Print a stored key to stdout (gated; for scripts/debugging)
infer auth import --from-env    Migrate keys from .env / environment into the encrypted store
    --from-env-file <path>      Import from a specific .env file
infer auth export --to-env-file <path>   (Optional) Decrypt to a .env for emergency/portability
```

`infer auth` is a Cobra parent command with subcommands, mirroring the existing `infer mcp` idiom in
[`cmd/mcp.go`](../../cmd/mcp.go). Register it from a new `cmd/auth.go` via `rootCmd.AddCommand(authCmd)`.

Provider names are friendly (`anthropic`, `openai`, `google`, `groq`, `mistral`, `deepseek`, `cohere`, `cloudflare`,
`ollama`, `ollama-cloud`, `minimax`, `moonshot`, plus the search providers) and resolve to canonical env-var names via
the [provider registry](#provider-registry-single-source-of-truth).

### Storage Backends

A `CredentialStore` abstraction with three concrete backends and a resolver that ties them together:

```text
domain.CredentialStore  (interface: Get/Set/Delete/List for a provider key)
├── keyringStore   → zalando/go-keyring, service "infer", account = env-var name
├── fileStore      → encrypted file (Argon2id + XChaCha20-Poly1305), see below
└── envStore       → read-only view over os.Getenv / INFER_* (never writes)
```

**Backend selection (`--storage auto`, the default):**

1. **`keyring`** — try the OS keyring first. If a working keyring is present, use it (no passphrase needed).
2. **`file`** — if the keyring is unavailable (headless Linux without Secret Service, WSL, locked-down CI), use the
   encrypted file, prompting for the passphrase.
3. If the user forces a backend (`--storage keyring` or `--storage file`) we honor it and **fail with a clear error**
   rather than silently falling back, so behavior is deterministic in automation.

`--storage env` is read-only and exists mainly for `status`/`token` to show that a key is currently coming from the
environment rather than the store.

### Encrypted-File Format

Stored at `~/.infer/credentials.enc` (userspace) or `.infer/credentials.enc` (project, git-ignored). A single file
holds all providers (a map), so one passphrase unlock yields every key. Layout — a small, versioned, self-describing
envelope so KDF parameters can be upgraded later without breaking old files:

```jsonc
{
  "version": 1,
  "kdf": "argon2id",
  "kdf_params": { "memory_kib": 65536, "time": 3, "threads": 4, "key_len": 32 },
  "salt":  "<base64, 16 random bytes, unique per file>",
  "cipher": "xchacha20poly1305",
  "nonce": "<base64, 24 random bytes>",
  "ciphertext": "<base64 AEAD output over the JSON map of provider -> key>"
}
```

- The plaintext payload is a JSON object `{ "ANTHROPIC_API_KEY": "sk-...", "OPENAI_API_KEY": "sk-..." }`.
- Encryption key = `Argon2id(passphrase, salt, kdf_params)`; decryption recomputes it from the stored params + salt.
- The envelope itself is **non-secret** (salt, nonce, and params are safe to store in clear); only `ciphertext` is
  sensitive, and it is authenticated (tampering fails the AEAD check).
- File written `0600`, parent dir `0700`.

This is exactly the "salt + encrypted on disk in the home directory or the project's git-ignored path" the request
calls for, and it beats the file fallbacks of both keyring libraries (`gh` has none; `99designs/keyring` uses older
PBKDF2).

### Passphrase Handling

- **Interactive:** prompt with no echo via `golang.org/x/term` (already transitively vendored — `go.sum` shows
  `golang.org/x/term v0.44.0`). On first `login` to the file backend, confirm the passphrase twice.
- **Headless / CI:** read the passphrase from `INFER_AUTH_PASSPHRASE` (env var, never written to disk). If the file
  backend is selected and no passphrase is available, **fail closed** with an actionable message.
- **Caching within a process:** the derived key is held only for the lifetime of the command/session and zeroed after
  use; the passphrase is never logged or persisted.

### Provider Registry (Single Source of Truth)

Introduce one canonical list mapping friendly provider name → env-var name → category, and make every consumer read
from it: `infer auth`, `gateway_manager.go` (both `loadEnvironment` and the container `apiKeyEnvVars`), and the system
status output. This eliminates the `.env.example` ↔ `gateway_manager.go` drift (the missing MiniMax/Moonshot entries)
by construction.

```go
// internal/services/credentials/providers.go (illustrative)
type Provider struct {
    Name   string // "anthropic"
    EnvVar string // "ANTHROPIC_API_KEY"
    Kind   string // "model" | "search"
    Label  string // "Anthropic"
}
var Providers = []Provider{ /* anthropic, openai, google, groq, mistral, deepseek,
    cohere, cloudflare, ollama, ollama-cloud, minimax, moonshot, google-search, duckduckgo */ }
```

`.env.example` can then be **generated** from this registry (or validated against it in a test) so it can never drift
again.

### Credential Resolution at Gateway Start

When the gateway manager builds the child environment, resolve each provider key with a fixed precedence (highest
wins), so CI and power users are never blocked and nothing breaks:

```text
1. Process environment / INFER_* override   (os.Getenv — authoritative, CI-friendly, no disk)
2. OS keyring                               (CredentialStore keyring backend)
3. Encrypted file                           (CredentialStore file backend, if unlocked)
4. Legacy .env in the working directory     (existing loadEnvironment behavior — deprecated, still honored)
```

Concretely, `loadEnvironment()` and `runContainer()`'s `apiKeyEnvVars` loop are refactored to iterate the provider
registry and call the resolver, instead of hard-coding the slice and reading only `os.Getenv` + `.env`. The legacy
`.env` path stays as the lowest-priority source for one or more releases (see
[Backward Compatibility](#backward-compatibility-and-migration)).

## Implementation Plan

Phased so each step is reviewable and shippable on its own.

**Phase 1 — Credential store core (no UI).**

- New package `internal/services/credentials/`:
  - `store.go` — `CredentialStore` interface + `auto` resolver.
  - `keyring.go` — `zalando/go-keyring` backend (`service = "infer"`, account = env-var name).
  - `file.go` — encrypted-file backend (Argon2id + XChaCha20-Poly1305 envelope above), `0600`/`0700`.
  - `providers.go` — the provider registry.
  - `resolver.go` — the precedence chain (env → keyring → file → `.env`).
- New domain interface in [`internal/domain/interfaces.go`](../../internal/domain/interfaces.go) (this triggers a
  counterfeiter mock regen via pre-commit — see [CLAUDE.md](../../CLAUDE.md)).
- Unit tests with a temp `HOME`, an in-memory keyring (the `zalando` package ships a mock), and round-trip
  encrypt/decrypt + tamper-detection tests.

**Phase 2 — `infer auth` command.**

- `cmd/auth.go`: parent `authCmd` + `login`/`logout`/`status`/`list`/`token`/`import` subcommands, wired to the store.
- Passphrase prompt via `golang.org/x/term`; `INFER_AUTH_PASSPHRASE` for headless.
- `status`/`list` never print secret values; `token` prints only with an explicit provider argument.

**Phase 3 — Gateway integration.**

- Refactor [`internal/services/gateway_manager.go`](../../internal/services/gateway_manager.go) `loadEnvironment` and
  `runContainer` to resolve keys through the registry + resolver. Keep `.env` as the lowest-priority fallback.
- Plumb the store through the service container
  ([`internal/container/container.go`](../../internal/container/container.go)) with a `Get*()` accessor, per the
  container conventions in [CLAUDE.md](../../CLAUDE.md).

**Phase 4 — Migration + housekeeping.**

- `infer auth import --from-env` reads `.env` / environment and writes the encrypted store.
- Add `credentials.enc` to the seeded `.infer/.gitignore` template in
  [`cmd/init.go`](../../cmd/init.go) and to the committed [`.infer/.gitignore`](../../.infer/.gitignore) (defense in
  depth even though the file is encrypted).
- Generate/validate `.env.example` from the provider registry.

**Phase 5 — Config + docs.**

- `AuthConfig` in [`config/config.go`](../../config/config.go) (see below); regenerate config via
  `go run . init --overwrite` and **restore user-curated files** (`agents.yaml`, `mcp.yaml`, …) per
  [CLAUDE.md](../../CLAUDE.md).
- New `docs/authentication.md`; update `docs/commands-reference.md`, `docs/configuration-reference.md`, and
  `docs/directory-structure.md`. File the `[DOCS]` ticket (below).

**Files touched (summary):**

| Action | Path |
| --- | --- |
| Add | `internal/services/credentials/{store,keyring,file,providers,resolver}.go` (+ tests) |
| Add | `cmd/auth.go` (+ `cmd/auth_test.go`) |
| Add | `docs/authentication.md` |
| Modify | `internal/services/gateway_manager.go` (resolve via registry/resolver) |
| Modify | `internal/domain/interfaces.go` (+ regenerated mock in `tests/mocks/`) |
| Modify | `internal/container/container.go` (accessor + wiring) |
| Modify | `config/config.go` (`AuthConfig`) |
| Modify | `cmd/init.go`, `.infer/.gitignore` (ignore `credentials.enc`) |
| Modify | `.env.example` (generated from registry), `docs/{commands,configuration,directory-structure}-reference/...` |

## Configuration Changes

Add a non-secret `auth` block to `config.yaml` (secrets never live here — only metadata):

```yaml
auth:
  storage: auto          # auto | keyring | file  (default backend selection)
  file_path: ""          # optional override; default ~/.infer/credentials.enc or .infer/credentials.enc
  prefer_project: false  # when using the file backend, default to ./.infer over ~/.infer
```

This follows the env-override format already documented in [CLAUDE.md](../../CLAUDE.md)
(`auth.storage` → `INFER_AUTH_STORAGE`). Validation rejects unknown `storage` values at config-load time, matching the
existing `approval_behaviour` validation pattern. Per the repo workflow, after editing `config/config.go` we run
`go run . init --overwrite` and discard the regenerated user-data YAMLs.

## Security Considerations

- **Threat model.** Protect provider keys from casual local disclosure (other users/processes, backups, sync clients,
  accidental `git add`). It does **not** defend against a fully compromised account that can also key-log the
  passphrase or read the OS keyring as the user — no local at-rest scheme can.
- **No plaintext default.** The happy path writes only ciphertext (file backend) or hands the secret to the OS keyring.
  The legacy `.env` remains supported but is documented as the insecure/deprecated path.
- **Fail closed.** If a forced backend is unusable (no keyring; no passphrase for the file), the command errors instead
  of degrading to plaintext — consistent with the headless secure-by-default behavior in
  [CLAUDE.md](../../CLAUDE.md).
- **AEAD + per-file salt.** Authenticated encryption detects tampering; the random per-file salt defeats precomputed /
  rainbow attacks and makes identical passphrases yield distinct keys across machines.
- **Permissions & memory.** `0600`/`0700`; derived keys and passphrase buffers are zeroed after use; secrets are never
  logged.
- **Interplay with the bash env-leak guard.** The existing guard already blocks `echo $ANTHROPIC_API_KEY`-style
  exfiltration (see [CLAUDE.md](../../CLAUDE.md)); `infer auth token` is the sanctioned, explicit way to read a key,
  and it requires a named provider argument.
- **Redaction.** `status`/`list` show presence and backend only; values are masked.

## Backward Compatibility and Migration

- **Nothing breaks.** `.env`, shell-exported vars, and `INFER_*` continue to work; they sit at the top of the
  [resolution precedence](#credential-resolution-at-gateway-start), so anyone relying on them is unaffected.
- **Opt-in.** The encrypted store is only consulted if the user runs `infer auth login` (or `import`). No forced
  migration.
- **Guided migration.** `infer auth import --from-env` reads the current `.env`/environment, writes the encrypted
  store, and prints a reminder to delete `.env`.
- **Deprecation, not removal.** The legacy `.env` path stays for at least one minor release with a deprecation note;
  removal (if ever) is a separate, announced change.

## Testing Strategy

- **Crypto round-trip:** encrypt → decrypt returns the original map; wrong passphrase fails cleanly; flipped
  ciphertext byte fails the AEAD check; salt/nonce uniqueness across writes.
- **Backends:** keyring backend against the `zalando` in-memory mock; file backend against a temp `HOME`; `auto`
  resolver picks the right backend and respects forced selection.
- **Resolver precedence:** env beats keyring beats file beats `.env`.
- **Command tests:** `login --with-token` (stdin), `status`/`list` redaction, `import --from-env` round-trip, headless
  `INFER_AUTH_PASSPHRASE` path, fail-closed on missing passphrase.
- **Gateway integration:** `loadEnvironment`/`runContainer` emit the resolved keys for every registry provider
  (including MiniMax/Moonshot — a regression test for the current drift).
- **Registry ↔ `.env.example`:** a test asserting they stay in sync.
- Run via `flox activate -- task test`; regenerate mocks with `flox activate -- task mocks:generate` after touching
  `internal/domain/interfaces.go`.

## Cross-Repo Impact and Docs Follow-Up

- **`inference-gateway/cli`** — all code changes land here. This is the only repo with code changes.
- **`inference-gateway/docs`** — per the maintainer docs-follow-up rule, this `feat:` needs a matching **`[DOCS]`**
  ticket: document `infer auth`, the encrypted store, the backend selector, and the migration from `.env`. Affected
  pages: a new authentication guide plus updates to the commands, configuration, and directory-structure references.
- **`inference-gateway` (gateway server)** — no change required; the CLI keeps injecting the same env-var names the
  gateway already reads. If the gateway ever adds providers, the CLI registry should be updated in lockstep (and is now
  the single place to do it).
- **`schemas`** — not affected; this introduces no API/OpenAPI surface.

No cross-repo issues are filed automatically by this proposal; the `[DOCS]` ticket is created alongside the
implementing PR.

## Dependencies

- **`golang.org/x/crypto`** — already a direct dependency (`v0.53.0`): provides `argon2`, `chacha20poly1305`,
  `nacl/secretbox`, and `scrypt`. **No new dependency** for the encryption itself.
- **`golang.org/x/term`** — already transitively present (`go.sum`: `v0.44.0`) for the no-echo passphrase prompt.
- **`github.com/zalando/go-keyring`** — the one **new** direct dependency, for OS-keyring access. Chosen over
  `99designs/keyring` because it is small, pure-keychain (no transitive D-Bus/JOSE stack), and is the same library
  `gh` uses; our encrypted-file fallback is built on `x/crypto` rather than the heavier library's PBKDF2 file backend.
  _Alternative considered:_ `99designs/keyring`, if we later want its broader backend set (KWallet, `pass`, KeyCtl)
  out of the box.

## Open Questions

1. **One file vs. per-provider files / keyring entries.** A single `credentials.enc` means one passphrase unlock for
   all keys (proposed). Per-provider keyring accounts are still used on the keyring backend. Confirm the single-file
   choice for the file backend.
2. **Passphrase caching.** Should a short-lived agent (`infer-agent` cron/heartbeat) be able to cache the passphrase
   for its run, or always require `INFER_AUTH_PASSPHRASE`? Proposed: env var only for headless; no on-disk cache.
3. **Search keys in the same store.** Include `GOOGLE_SEARCH_API_KEY` / `DUCKDUCKGO_SEARCH_API_KEY` as
   `Kind: "search"` providers (proposed) so the tool-level `os.Getenv` lookups can also resolve from the store.
4. **External credential helpers.** Worth a follow-up: a Docker/AWS-style `credential_process` escape hatch for
   enterprises with their own secret managers.
5. **`infer auth export`.** Include a guarded decrypt-to-`.env` for portability/emergencies, or omit to avoid
   re-introducing a plaintext path? Proposed: include but gate behind an explicit `--to-env-file` and a warning.

## References

Prior-art and crypto sources (official docs or repository source):

- GitHub CLI — `gh auth` subcommands and keyring-first storage with `hosts.yml` fallback:
  <https://cli.github.com/manual/gh_auth_login>, <https://github.com/cli/cli/blob/trunk/internal/config/config.go>,
  <https://github.com/cli/cli/blob/trunk/internal/keyring/keyring.go>
- Stripe CLI — livemode→keyring / testmode→file split, `99designs/keyring`:
  <https://github.com/stripe/stripe-cli/blob/master/pkg/config/profile.go>
- Docker CLI — `config.json`, `credsStore`/`credHelpers`, base64 (not encryption), helper protocol:
  <https://github.com/docker/cli/blob/master/cli/config/configfile/file.go>,
  <https://github.com/docker/docker-credential-helpers/blob/master/README.md>
- AWS CLI — plaintext `~/.aws/credentials`:
  <https://docs.aws.amazon.com/cli/latest/userguide/cli-configure-files.html>
- Vercel CLI — `credStorage: auto|file|keyring`:
  <https://github.com/vercel/vercel/blob/main/packages/cli-config/src/cred-storage.ts>
- flyctl `config.yml`: <https://github.com/superfly/flyctl/blob/master/internal/config/config.go> ·
  Heroku netrc: <https://devcenter.heroku.com/articles/heroku-cli-commands> ·
  npm `_authToken`: <https://github.com/npm/cli/blob/latest/docs/lib/content/configuring-npm/npmrc.md>
- Go keyring libraries — `zalando/go-keyring`: <https://github.com/zalando/go-keyring> ·
  `99designs/keyring` (incl. file backend crypto): <https://github.com/99designs/keyring/blob/master/file.go>
- Crypto guidance — OWASP Password Storage Cheat Sheet (Argon2id/scrypt params, salting):
  <https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html> ·
  Argon2 RFC 9106: <https://www.rfc-editor.org/rfc/rfc9106> ·
  `golang.org/x/crypto` (argon2, chacha20poly1305, nacl, scrypt): <https://pkg.go.dev/golang.org/x/crypto>
