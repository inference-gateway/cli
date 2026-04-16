# Nix Packaging for Inference Gateway CLI

This directory contains the Nix package expression for building the Inference Gateway CLI with Nix.

## Quick Start

### Building Locally

```bash
# Build the package
nix-build nix/infer.nix

# Test the binary
./result/bin/infer version
./result/bin/infer --help

# Install to user profile
nix-env -if nix/infer.nix
```

### Testing Before Release

Before each release, ensure the Nix package builds correctly:

```bash
# Clean build
nix-build nix/infer.nix --show-trace

# Verify the binary works
./result/bin/infer version
./result/bin/infer chat --help

# Check shell completions were generated
ls -la ./result/share/bash-completion/completions/
ls -la ./result/share/fish/vendor_completions.d/
ls -la ./result/share/zsh/site-functions/
```

## Updating Hashes

The package expression contains two hashes that must be updated for each release:

### 1. Source Hash

This is the hash of the GitHub source tarball:

```bash
# Calculate for a specific version
VERSION="0.76.1"
nix-prefetch-url --unpack "https://github.com/inference-gateway/cli/archive/refs/tags/v${VERSION}.tar.gz"

# Update in infer.nix:
# hash = "sha256-CALCULATED_HASH";
```

### 2. Vendor Hash

This is the hash of the Go module dependencies:

```bash
# Set vendorHash to empty string in infer.nix
sed -i 's|vendorHash = "sha256-.*";|vendorHash = "";|' infer.nix

# Attempt to build - it will fail with the correct hash
nix-build nix/infer.nix 2>&1 | tee build.log

# Extract the hash from the error
grep "got:" build.log | grep -oP "sha256-[A-Za-z0-9+/=]+"

# Update in infer.nix:
# vendorHash = "sha256-CALCULATED_HASH";
```

## Automated Workflow

The `.github/workflows/nix-version-sync.yml` workflow automatically:

1. Triggers on new releases
2. Calculates both hashes
3. Updates `nix/infer.nix`
4. Creates a PR with the changes
5. Verifies the build succeeds

You can also trigger it manually:

```bash
# Via GitHub UI: Actions > Nix Version Sync > Run workflow
# Or via gh CLI:
gh workflow run nix-version-sync.yml -f version=0.76.1
```

## CI Integration

The `.github/workflows/nix-build.yml` workflow runs on every PR and push to verify:

- Nix package builds on Linux (amd64, arm64)
- Nix package builds on macOS (amd64, arm64)
- Binary runs and `infer version` works
- Nix expression is properly formatted

## Platform Support

The package supports:

- **Linux**: x86_64-linux, aarch64-linux
- **macOS**: x86_64-darwin, aarch64-darwin (requires CGO for clipboard)

### macOS Notes

macOS builds require CGO enabled for clipboard support (`golang.design/x/clipboard`):

```nix
CGO_ENABLED = if stdenv.isDarwin then 1 else 0;

buildInputs = lib.optionals stdenv.isDarwin [
  darwin.apple_sdk.frameworks.Cocoa
  darwin.apple_sdk.frameworks.UserNotifications
];
```

## Submitting to nixpkgs

Once the package builds successfully:

1. Follow the [nixpkgs submission guide](../docs/nixpkgs-submission.md)
2. Ensure all hashes are correct (no placeholders)
3. Test on at least NixOS Linux and macOS
4. Submit PR to [NixOS/nixpkgs](https://github.com/NixOS/nixpkgs)

## Troubleshooting

### Build Fails with Hash Mismatch

```bash
# Recalculate the hash
nix-prefetch-url --unpack "https://github.com/inference-gateway/cli/archive/refs/tags/vVERSION.tar.gz"
```

### Vendor Hash Mismatch

```bash
# Set to empty string and rebuild to get correct hash
sed -i 's|vendorHash = ".*";|vendorHash = "";|' nix/infer.nix
nix-build nix/infer.nix 2>&1 | grep "got:"
```

### CGO Errors on macOS

Ensure:

- `CGO_ENABLED = 1` for Darwin
- `darwin.apple_sdk.frameworks.Cocoa` in buildInputs
- Xcode Command Line Tools are installed

### Tests Fail in Sandbox

Some tests may require network or fail in Nix sandbox. Use `checkFlags`:

```nix
checkFlags = [
  "-skip=TestIntegration|TestNetwork"
];
```

## Development

### Local Testing with Different Go Versions

```bash
# Override Go version
nix-build nix/infer.nix --arg go go_1_23

# With specific nixpkgs version
nix-build nix/infer.nix -I nixpkgs=https://github.com/NixOS/nixpkgs/archive/nixos-24.11.tar.gz
```

### Formatting

Format Nix files with nixpkgs-fmt:

```bash
nix-shell -p nixpkgs-fmt --run "nixpkgs-fmt nix/"
```

### Checking Evaluation

Ensure the expression evaluates without errors:

```bash
nix-instantiate --eval --strict nix/infer.nix --show-trace
```

## Resources

- [Nix Pills](https://nixos.org/guides/nix-pills/)
- [nixpkgs Manual - Go](https://nixos.org/manual/nixpkgs/stable/#sec-language-go)
- [nixpkgs Contributing](https://github.com/NixOS/nixpkgs/blob/master/CONTRIBUTING.md)
- [Cachix](https://www.cachix.org/) - Binary cache for faster builds

## Contact

For issues with the Nix package:

- Open an issue in this repository
- Tag with `nix` label
- Include build logs and system info
