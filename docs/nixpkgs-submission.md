# nixpkgs Submission Guide

This guide walks you through submitting the Inference Gateway CLI to the official NixOS/nixpkgs repository.

## Prerequisites

Before submitting, ensure:

- ✅ The Nix package builds successfully locally
- ✅ The CI workflow (`.github/workflows/nix-build.yml`) passes
- ✅ You have a GitHub account
- ✅ You're familiar with Git and GitHub pull requests
- ✅ You've reviewed the [nixpkgs contributing guidelines](https://github.com/NixOS/nixpkgs/blob/master/CONTRIBUTING.md)

## Step 1: Verify Local Build

First, test the Nix package builds correctly:

```bash
# Build the package
nix-build nix/infer.nix

# Test the binary
result/bin/infer version
result/bin/infer --help

# Run a quick smoke test
result/bin/infer chat --help
```

If any issues arise, fix them before proceeding.

## Step 2: Calculate Final Hashes

The `nix/infer.nix` file contains placeholder hashes that need to be calculated:

```bash
# Source hash (already calculated by nix-version-sync workflow)
# Verify it's correct:
nix-prefetch-url --unpack https://github.com/inference-gateway/cli/archive/refs/tags/v0.76.1.tar.gz

# Vendor hash (also calculated by workflow)
# To manually verify:
sed -i 's|vendorHash = ".*";|vendorHash = "";|' nix/infer.nix
nix-build nix/infer.nix 2>&1 | grep "got:" | grep -oP "sha256-[A-Za-z0-9+/=]+"
```

## Step 3: Fork nixpkgs

1. Go to <https://github.com/NixOS/nixpkgs>
2. Click "Fork" in the top-right corner
3. Clone your fork:

```bash
git clone https://github.com/YOUR_USERNAME/nixpkgs.git
cd nixpkgs
```

## Step 4: Create Package in nixpkgs

nixpkgs now uses the `pkgs/by-name` structure for new packages:

```bash
# Create the package directory
mkdir -p pkgs/by-name/in/infer

# Copy the Nix expression
cp /path/to/cli/nix/infer.nix pkgs/by-name/in/infer/package.nix

# Note: In nixpkgs, the file MUST be named "package.nix"
```

### Edit package.nix

Update the file for nixpkgs conventions:

```nix
{ lib
, buildGoModule
, fetchFromGitHub
, installShellFiles
, stdenv
, darwin
}:

buildGoModule rec {
  pname = "infer";
  version = "0.76.1";

  src = fetchFromGitHub {
    owner = "inference-gateway";
    repo = "cli";
    rev = "v${version}";
    hash = "sha256-ACTUAL_HASH_HERE";  # Use real hash
  };

  vendorHash = "sha256-ACTUAL_VENDOR_HASH_HERE";  # Use real hash

  # ... rest of the file
}
```

**Important changes for nixpkgs:**

1. Remove any GitHub Actions or CI-specific comments
2. Ensure all hashes are correct (no placeholders)
3. Add yourself to `meta.maintainers`:

```nix
maintainers = with maintainers; [ your-github-username ];
```

## Step 5: Test the Package in nixpkgs

Build and test from within the nixpkgs repository:

```bash
# Build the package
nix-build -A infer

# Test it
result/bin/infer version

# Check metadata
nix-instantiate --eval -E 'with import ./. {}; infer.meta.description'

# Run nixpkgs checks
nix-build -A infer.tests
```

## Step 6: Create Commit and Branch

Follow nixpkgs commit conventions:

```bash
# Create a branch
git checkout -b infer-init

# Add the package
git add pkgs/by-name/in/infer/package.nix

# Commit with proper message
git commit -m "infer: init at 0.76.1"
```

**Commit message format:**

- For new packages: `<pname>: init at <version>`
- For updates: `<pname>: <old-version> -> <new-version>`

## Step 7: Push and Create Pull Request

```bash
# Push to your fork
git push origin infer-init
```

Go to <https://github.com/YOUR_USERNAME/nixpkgs> and create a pull request.

### PR Template

Use this template for your PR description:

```markdown
#### Description of changes

This PR adds the Inference Gateway CLI (`infer`), a command-line tool for managing AI model interactions.

#### Package Details

- **Package name**: `infer`
- **Version**: `0.76.1`
- **License**: MIT
- **Platforms**: Linux (x86_64, aarch64), macOS (x86_64, aarch64)
- **Homepage**: https://github.com/inference-gateway/cli

#### Features

- Interactive chat with AI models
- Autonomous agent execution with tool support
- Multiple storage backends (SQLite, PostgreSQL, Redis)
- Terminal UI with BubbleTea framework
- Extensive tool system

#### Testing

Tested on:
- [x] NixOS 24.11 (x86_64-linux)
- [x] macOS 14 Sonoma (aarch64-darwin)

#### Checklist

- [x] Built on NixOS
- [x] Built on macOS (if supported)
- [x] Binary runs and `infer version` works
- [x] Shell completions generated
- [x] All hashes are correct
- [x] `meta.description` is set
- [x] `meta.license` is correct
- [x] `meta.maintainers` includes me
- [x] Package follows [Go packaging guidelines](https://github.com/NixOS/nixpkgs/blob/master/doc/languages-frameworks/go.section.md)

#### Build instructions

```bash
nix-build -A infer
result/bin/infer version
```

---

cc @NixOS/go-maintainers for review

## Step 8: Respond to Review Feedback

nixpkgs maintainers will review your PR. Common feedback:

1. **Hash mismatches**: Recalculate and update
2. **Build failures**: Check build logs and fix
3. **Metadata issues**: Update `meta` attributes
4. **Naming conventions**: Ensure pname matches expectations
5. **Platform support**: Verify claimed platforms actually work

Be responsive and make requested changes promptly.

## Step 9: Maintenance After Merge

Once merged, you're responsible for maintaining the package:

### Updating to New Versions

1. Wait for the nix-version-sync workflow to create a PR in the CLI repo
2. Merge that PR to update `nix/infer.nix`
3. Create an update PR in nixpkgs:

```bash
cd nixpkgs
git checkout master
git pull upstream master
git checkout -b infer-0.77.0

# Update version and hashes in pkgs/by-name/in/infer/package.nix
# Copy from the updated nix/infer.nix in the CLI repo

git add pkgs/by-name/in/infer/package.nix
git commit -m "infer: 0.76.1 -> 0.77.0"
git push origin infer-0.77.0
```

4. Create PR with update

### Automated Updates

Consider using [nixpkgs-update](https://github.com/ryantm/nixpkgs-update) bot or setting up automation to detect new releases.

### Handling Issues

Monitor:

- GitHub issues in the CLI repo that mention NixOS
- nixpkgs issues mentioning `infer`
- Build failures in Hydra (nixpkgs CI)

## Useful Resources

- [nixpkgs Manual - Go](https://nixos.org/manual/nixpkgs/stable/#sec-language-go)
- [nixpkgs Contributing Guide](https://github.com/NixOS/nixpkgs/blob/master/CONTRIBUTING.md)
- [Nix Pills](https://nixos.org/guides/nix-pills/) - Deep dive into Nix
- [nixpkgs Go Packaging](https://github.com/NixOS/nixpkgs/blob/master/doc/languages-frameworks/go.section.md)
- [Ofborg Commands](https://github.com/NixOS/ofborg) - nixpkgs CI bot

## Quick Reference

### Build Commands

```bash
# Local build in CLI repo
nix-build nix/infer.nix

# Build in nixpkgs
nix-build -A infer

# Build for specific platform
nix-build -A infer --arg system "aarch64-darwin"

# Check what will be built
nix-instantiate -A infer
```

### Hash Calculation

```bash
# Source hash
nix-prefetch-url --unpack https://github.com/inference-gateway/cli/archive/refs/tags/vVERSION.tar.gz

# Vendor hash (build with empty string and read error)
nix-build -A infer 2>&1 | grep "got:" | grep -oP "sha256-[A-Za-z0-9+/=]+"
```

### Commit Messages

```text
infer: init at 0.76.1           # New package
infer: 0.76.1 -> 0.77.0         # Version update
infer: fix build on aarch64     # Bug fix
infer: add shell completions    # Enhancement
```

## Troubleshooting

### Build fails with "hash mismatch"

Recalculate the hash:

```bash
nix-prefetch-url --unpack TARBALL_URL
```

### "vendor hash mismatch"

Set `vendorHash = "";` and rebuild to get the correct hash from the error message.

### "package not found"

Ensure the package is in `pkgs/by-name/in/infer/package.nix` and the directory structure is correct.

### CGO errors on macOS

Ensure `darwin.apple_sdk.frameworks.Cocoa` is in `buildInputs` and `CGO_ENABLED = 1` for Darwin.

### Tests fail in sandbox

Some tests may require network or fail in the Nix sandbox. Use `checkFlags` to skip them:

```nix
checkFlags = [
  "-skip=TestIntegration|TestNetwork"
];
```

## Contact

- **CLI Repository**: <https://github.com/inference-gateway/cli>
- **nixpkgs Issues**: <https://github.com/NixOS/nixpkgs/issues>
- **NixOS Discourse**: <https://discourse.nixos.org/>
