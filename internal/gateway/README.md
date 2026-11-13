# gateway

**What** - the gateway bounded context: running the Inference Gateway the CLI talks to, as a binary from
`~/.infer/bin` or as a container, and keeping that binary the release the configuration asks for.
**Why** - the CLI, Desktop and a workspace pod share one binary in one directory, so one owner decides
which release is installed and when it is replaced.
**How** - `domain/` holds the release choice: what the configuration wants (`WantedRelease`), what the
cached binary says about itself (`CachedBinary`) and which release to install. `infrastructure/` holds
`BinaryInstaller`, the one place the gateway is downloaded: it asks GitHub for the latest release, probes
the cached binary and installs a download as a new file. The package itself is the `Supervisor`, which
starts, watches and stops the gateway.

## How it plugs in

- `gateway.version` chooses the release, `latest` by default. A pinned release is installed exactly and
  never updated past, and needs no release lookup.
- The `Supervisor` asks the installer for the binary before it starts it. `infer gateway install` asks
  for it without starting anything, which is how Desktop installs the gateway.
- A download is written beside the binary and renamed into place. macOS ends a signed binary whose file
  was rewritten in place every time it starts.
- Container mode installs nothing: the image tag in `gateway.oci` chooses the release.

## Related

- [Configuration Reference](../../docs/configuration-reference.md#gateway-settings)
- [Commands Reference](../../docs/commands-reference.md#infer-gateway)
