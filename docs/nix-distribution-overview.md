# NixOS Package Distribution - Implementation Overview

This document provides a comprehensive overview of the NixOS package distribution setup for the Inference Gateway CLI.

## What Was Implemented

### 1. Nix Package Expression (`nix/infer.nix`)

A complete Nix derivation that:

- Uses `buildGoModule` for Go package building
- Fetches source from GitHub releases
- Handles CGO requirements for macOS (clipboard support)
- Generates shell completions (bash, fish, zsh)
- Sets proper ldflags for version information
- Includes comprehensive metadata for nixpkgs

**Key Features:**

- Cross-platform support (Linux amd64/arm64, macOS amd64/arm64)
- Automatic dependency management via Go modules
- Security through reproducible builds
- Shell completion installation

### 2. CI/CD Integration

#### Nix Build Verification (`.github/workflows/nix-build.yml`)

Runs on every PR and push to verify:

- ✅ Package builds on all supported platforms
- ✅ Binary executes correctly (`infer version`)
- ✅ Nix formatting is correct
- ✅ Nix expression evaluates without errors

**Matrix Testing:**

- ubuntu-24.04 (linux-amd64, linux-arm64)
- macos-15 (darwin-amd64, Intel Mac)
- macos-latest (darwin-arm64, Apple Silicon)

**Optimizations:**

- Cachix integration for faster builds
- Parallel platform testing
- Artifact uploads for verification

#### Version Sync Automation (`.github/workflows/nix-version-sync.yml`)

Automatically updates the Nix package on releases:

- 🔄 Triggers on GitHub releases (or manual workflow dispatch)
- 🔢 Calculates source hash from GitHub tarball
- 📦 Determines vendorHash by building
- ✏️ Updates `nix/infer.nix` with new hashes
- 🎨 Formats with nixpkgs-fmt
- ✅ Verifies build succeeds
- 🔀 Creates PR with changes

**Benefits:**

- No manual hash calculation needed
- Automatic verification before PR
- Consistent formatting
- Clear audit trail

### 3. Documentation

#### nixpkgs Submission Guide (`docs/nixpkgs-submission.md`)

Comprehensive guide covering:

- Prerequisites and preparation
- Step-by-step submission process
- Local build verification
- Hash calculation methods
- Fork and PR workflow
- Review response guidelines
- Maintenance procedures
- Troubleshooting common issues

#### Nix README (`nix/README.md`)

Quick reference for:

- Local building and testing
- Hash update procedures
- Automated workflow usage
- Platform-specific considerations
- Development tips
- Common troubleshooting

#### README Updates

Added NixOS installation section to main README.md:

- Installation from nixpkgs (post-submission)
- Building from source with Nix
- Benefits of using Nix
- Link to submission guide

## Architecture Decisions

### Why These Choices?

1. **nixpkgs submission only** (no standalone flake)
   - Single source of truth
   - Official distribution channel
   - Discoverable via `nix search`
   - Integrated with NixOS ecosystem

2. **Automated version sync**
   - Reduces human error
   - Faster release cycle
   - Consistent process
   - Easy to audit

3. **Multi-platform CI**
   - Catches platform-specific issues early
   - Verifies CGO requirements (macOS)
   - Tests on actual target platforms
   - Provides build artifacts for verification

4. **`pkgs/by-name` structure** (for nixpkgs)
   - Modern nixpkgs convention
   - Better organization
   - Faster evaluation
   - Required for new packages

## Current Status

### ✅ Completed

- [x] Nix package expression with proper structure
- [x] CI workflow for build verification
- [x] Automated version sync workflow
- [x] Comprehensive documentation
- [x] README updates with NixOS instructions
- [x] .gitignore updates for Nix artifacts

### ⏳ Pending (Next Steps)

1. **Calculate Actual Hashes**
   - Replace placeholder hashes in `nix/infer.nix`
   - Source hash: `nix-prefetch-url --unpack TARBALL_URL`
   - Vendor hash: Build with empty string, extract from error

2. **Verify Build Locally**

   ```bash
   nix-build nix/infer.nix
   ./result/bin/infer version
   ```

3. **Test CI Workflows**
   - Push changes to trigger nix-build.yml
   - Verify all platforms build successfully
   - Check Cachix integration (requires auth token)

4. **Setup Cachix** (Optional but Recommended)
   - Create account at <https://cachix.org>
   - Create `inference-gateway` cache
   - Add `CACHIX_AUTH_TOKEN` to GitHub secrets
   - Significantly speeds up CI builds

5. **Submit to nixpkgs**
   - Follow `docs/nixpkgs-submission.md`
   - Fork NixOS/nixpkgs
   - Copy to `pkgs/by-name/in/infer/package.nix`
   - Add yourself to maintainers
   - Create PR with template
   - Respond to reviewer feedback

## File Structure

```text
inference-gateway/cli/
├── .github/
│   └── workflows/
│       ├── nix-build.yml          # CI verification
│       └── nix-version-sync.yml   # Auto-update on release
├── docs/
│   ├── nixpkgs-submission.md      # Submission guide
│   └── nix-distribution-overview.md  # This file
├── nix/
│   ├── infer.nix                  # Nix package expression
│   └── README.md                  # Quick reference
├── .gitignore                      # Updated with Nix artifacts
└── README.md                       # Updated with NixOS section
```

## Maintenance Workflow

### For Each Release

1. **Automatic** (via GitHub Actions):
   - Release gets published
   - `nix-version-sync.yml` triggers
   - Calculates hashes
   - Creates PR with updates
   - CI verifies build

2. **Manual** (you do):
   - Review and merge the auto-generated PR
   - Wait for CI to pass
   - Verify binary works

3. **nixpkgs Update** (after initial submission):
   - Create update PR in nixpkgs
   - Use new version and hashes from CLI repo
   - Follow same process as initial submission

### Monitoring

Keep an eye on:

- **CI Build Status**: Ensure nix-build.yml passes
- **Hash Updates**: Verify auto-PRs are created on releases
- **nixpkgs Issues**: Watch for user-reported problems
- **Hydra Builds**: nixpkgs CI may catch platform issues

## Benefits for Users

### Why Users Should Use the Nix Package

1. **Reproducibility**
   - Same build every time
   - Bit-for-bit identical across machines
   - No "works on my machine" issues

2. **Dependency Management**
   - All dependencies included
   - No conflicts with system packages
   - Automatic cleanup of old versions

3. **Easy Rollback**

   ```bash
   nix-env --rollback
   ```

4. **Multiple Versions**

   ```bash
   nix-env -iA nixpkgs.infer  # Latest
   nix-env -f channel:nixos-23.11 -iA infer  # Specific release
   ```

5. **System Integration**
   - Shell completions auto-installed
   - Proper FHS paths
   - Integrates with NixOS configuration

6. **Security**
   - Build from source, not pre-built binaries
   - Verified dependencies
   - No need to trust maintainer's build environment

## Troubleshooting

### Common Issues and Solutions

#### Issue: "hash mismatch" on source

**Solution:**

```bash
# Recalculate the hash
nix-prefetch-url --unpack "https://github.com/inference-gateway/cli/archive/refs/tags/v0.76.1.tar.gz"

# Update in nix/infer.nix
```

#### Issue: "vendor hash mismatch"

**Solution:**

```bash
# Set vendorHash to empty string
sed -i 's|vendorHash = ".*";|vendorHash = "";|' nix/infer.nix

# Build and capture error
nix-build nix/infer.nix 2>&1 | grep "got:" | grep -oP "sha256-[A-Za-z0-9+/=]+"

# Use that hash in infer.nix
```

#### Issue: CGO errors on macOS

**Solution:**
Ensure:

```nix
CGO_ENABLED = if stdenv.isDarwin then 1 else 0;

buildInputs = lib.optionals stdenv.isDarwin [
  darwin.apple_sdk.frameworks.Cocoa
  darwin.apple_sdk.frameworks.UserNotifications
];
```

#### Issue: Tests fail in Nix sandbox

**Solution:**
Skip network-dependent tests:

```nix
checkFlags = [
  "-skip=TestIntegration|TestNetwork"
];
```

#### Issue: nix-version-sync workflow fails

**Possible Causes:**

1. GitHub token lacks permissions → Check workflow permissions
2. Version format incorrect → Ensure tags follow `vX.Y.Z` format
3. Build fails → Check Go module issues or missing dependencies

## Resources

### Official Documentation

- [Nix Manual](https://nixos.org/manual/nix/stable/)
- [nixpkgs Manual](https://nixos.org/manual/nixpkgs/stable/)
- [NixOS Manual](https://nixos.org/manual/nixos/stable/)

### Community

- [NixOS Discourse](https://discourse.nixos.org/)
- [NixOS Wiki](https://nixos.wiki/)
- [r/NixOS](https://www.reddit.com/r/NixOS/)

### Tools

- [nix-prefetch-url](https://nixos.org/manual/nix/stable/command-ref/nix-prefetch-url.html)
- [nixpkgs-fmt](https://github.com/nix-community/nixpkgs-fmt)
- [Cachix](https://cachix.org/)
- [nix-output-monitor](https://github.com/maralorn/nix-output-monitor)

### Related Projects

- [gomod2nix](https://github.com/nix-community/gomod2nix) - Alternative Go packaging
- [nixpkgs-update](https://github.com/ryantm/nixpkgs-update) - Automated updates
- [niv](https://github.com/nmattia/niv) - Dependency management

## Contact and Support

For issues with the Nix packaging:

- **Nix-specific issues**: Open in this repo with `nix` label
- **nixpkgs issues**: Open in NixOS/nixpkgs, mention `@maintainers/go`
- **General CLI issues**: Regular issue tracker

---

**Last Updated**: 2025-12-01
**Status**: Ready for hash calculation and submission
**Next Action**: Calculate actual hashes and verify local build
