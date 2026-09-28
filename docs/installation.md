# Installation

[← Back to README](../README.md)

**What** - every supported way to get the `infer` binary onto your machine.
**Why** - the channels trade convenience against repeatability and control: a quick look, a pinned production install, or a source build for CLI development.
**How** - use npm/npx to try it, the install script, Nix, or a container image for production, and build from source when you are working on the CLI itself.

## Using npm/npx (recommended)

If you already have Node.js (>= 18), run the CLI with npx - no Go toolchain or manual
download required. The matching native binary is fetched and cached on first use:

```bash
# Run without installing
npx @inference-gateway/cli@latest --help
npx @inference-gateway/cli@latest chat
```

Or install it globally:

```bash
npm install -g @inference-gateway/cli
infer --help
```

> **Not recommended for production.** For production or CI, prefer the install script,
> Nix flake, container image, or a source build below. Prebuilt binaries cover Linux,
> macOS, and Windows on amd64/arm64.

## Using Go Install

```bash
go install github.com/inference-gateway/cli/cmd/infer@latest
```

This installs the binary as `infer`.

## Using Nix Flake / Flox

With Nix (flakes enabled), run directly without installing:

```bash
nix run github:inference-gateway/cli

# Pin a specific release
nix run github:inference-gateway/cli/v0.217.0
```

Or install into your profile:

```bash
nix profile install github:inference-gateway/cli
```

With [Flox](https://flox.dev), pin it in your environment manifest (`.flox/env/manifest.toml`):

```toml
[install]
infer.flake = "github:inference-gateway/cli"
```

Then `flox activate` makes `infer` available in the environment. Pin a release by appending the tag: `github:inference-gateway/cli/v0.217.0`.

## Using Container Image

```bash
# Create network and deploy inference gateway first
docker network create inference-gateway
docker run -d --name inference-gateway --network inference-gateway \
  --env-file .env \
  ghcr.io/inference-gateway/inference-gateway:latest

# Pull and run the CLI
docker pull ghcr.io/inference-gateway/cli:latest
docker run -it --rm --network inference-gateway ghcr.io/inference-gateway/cli:latest chat
```

## Using Install Script

**Linux/macOS:**

```bash
# Latest version
curl -fsSL https://raw.githubusercontent.com/inference-gateway/cli/main/install.sh | bash

# Specific version
curl -fsSL https://raw.githubusercontent.com/inference-gateway/cli/main/install.sh | bash -s -- --version v0.217.0

# Custom installation directory
curl -fsSL https://raw.githubusercontent.com/inference-gateway/cli/main/install.sh | bash -s -- --install-dir $HOME/.local/bin
```

**Windows (PowerShell 5.1+ / pwsh):**

```powershell
# Latest version
.\install.ps1

# Specific version
.\install.ps1 -Version v0.1.0

# Custom installation directory
$env:INSTALL_DIR = "C:\tools"; .\install.ps1
```

Or run directly from GitHub:

```powershell
# Download and run
iex ((New-Object System.Net.WebClient).DownloadString('https://raw.githubusercontent.com/inference-gateway/cli/main/install.ps1'))
```

## Manual Download

Download the latest release binary for your platform from the [releases page](https://github.com/inference-gateway/cli/releases).

Available binaries:

| Platform | Binary |
| -------- | ------ |
| Linux amd64 | `infer-linux-amd64` |
| Linux arm64 | `infer-linux-arm64` |
| macOS amd64 (Intel) | `infer-darwin-amd64` |
| macOS arm64 (Apple Silicon) | `infer-darwin-arm64` |
| Windows amd64 | `infer-windows-amd64` (rename to `infer.exe`) |
| Windows arm64 | `infer-windows-arm64` (rename to `infer.exe`) |

Verify the download against `checksums.txt` or its Cosign signature before installing - see the
[Binary Verification Guide](binary-verification.md) - then `chmod +x` it and move it onto your `PATH` as `infer`.

## Build from Source

```bash
git clone https://github.com/inference-gateway/cli.git
cd cli
go build -o infer ./cmd/infer
sudo mv infer /usr/local/bin/
```

On Windows, build with:

```powershell
git clone https://github.com/inference-gateway/cli.git
cd cli
go build -o infer.exe ./cmd/infer
# The binary is at .\infer.exe
```

See [CONTRIBUTING.md](../CONTRIBUTING.md) for the development workflow.

## Next Steps

- [Quick Start](../README.md#quick-start) - first chat in three commands
- [Configuration Reference](configuration-reference.md) - providers, models, tools, and environment variables
- [Directory Structure](directory-structure.md) - every file and directory the CLI creates under `.infer/` and `~/.infer/`
