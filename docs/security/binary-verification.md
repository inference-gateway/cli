# Binary Verification Guide

[← Back to README](../../README.md)

This document provides detailed instructions for verifying the integrity and authenticity of Inference
Gateway CLI release binaries using SHA256 checksums and Cosign signatures.

## Why Verify Binaries?

Verifying release binaries ensures that:

1. **Integrity**: The binary hasn't been corrupted during download
2. **Authenticity**: The binary was genuinely released by the project maintainers
3. **Supply Chain Security**: Protection against supply chain attacks and compromised binaries

All official Inference Gateway CLI releases are signed with [Cosign](https://github.com/sigstore/cosign)
to provide cryptographic verification of authenticity.

---

## Verification Methods

We provide two verification methods:

1. **SHA256 Checksum Verification** (Basic): Verifies file integrity
2. **Cosign Signature Verification** (Advanced): Verifies authenticity and integrity

---

## SHA256 Checksum Verification

This method verifies that the binary hasn't been corrupted during download.

### Step 1: Download the Binary and Checksums

```bash
# Download binary (replace with your platform)
curl -L -o infer-darwin-amd64 \
  https://github.com/inference-gateway/cli/releases/latest/download/infer-darwin-amd64

# Download checksums file
curl -L -o checksums.txt \
  https://github.com/inference-gateway/cli/releases/latest/download/checksums.txt
```

### Step 2: Verify the Checksum

```bash
# Calculate checksum of downloaded binary
shasum -a 256 infer-darwin-amd64

# Compare with checksums in checksums.txt
grep infer-darwin-amd64 checksums.txt
```

The output from both commands should match exactly. If they differ, **do not use the binary** and try downloading again.

### Step 3: Install the Binary

Once verified, make the binary executable and install it:

```bash
chmod +x infer-darwin-amd64
sudo mv infer-darwin-amd64 /usr/local/bin/infer
```

---

## Cosign Signature Verification

This advanced method provides cryptographic verification that the binary was actually released by the
project maintainers, protecting against supply chain attacks.

### Prerequisites

Install [Cosign](https://github.com/sigstore/cosign):

```bash
# macOS
brew install cosign

# Linux (using release binary)
wget https://github.com/sigstore/cosign/releases/latest/download/cosign-linux-amd64
chmod +x cosign-linux-amd64
sudo mv cosign-linux-amd64 /usr/local/bin/cosign

# Or install via go
go install github.com/sigstore/cosign/v2/cmd/cosign@latest
```

### Step 1: Download All Verification Files

```bash
# Download binary (replace with your platform)
curl -L -o infer-darwin-amd64 \
  https://github.com/inference-gateway/cli/releases/latest/download/infer-darwin-amd64

# Download checksums and signature files
curl -L -o checksums.txt \
  https://github.com/inference-gateway/cli/releases/latest/download/checksums.txt
curl -L -o checksums.txt.pem \
  https://github.com/inference-gateway/cli/releases/latest/download/checksums.txt.pem
curl -L -o checksums.txt.sig \
  https://github.com/inference-gateway/cli/releases/latest/download/checksums.txt.sig
```

### Step 2: Verify SHA256 Checksum

First, verify the basic checksum as described in the SHA256 section above:

```bash
# Calculate checksum of downloaded binary
shasum -a 256 infer-darwin-amd64

# Compare with checksums in checksums.txt
grep infer-darwin-amd64 checksums.txt
```

### Step 3: Verify Cosign Signature

Now verify that the checksums file was signed by the project's official release workflow:

```bash
# Decode base64 encoded certificate
cat checksums.txt.pem | base64 -d > checksums.txt.pem.decoded

# Verify the signature
cosign verify-blob \
  --certificate checksums.txt.pem.decoded \
  --signature checksums.txt.sig \
  --certificate-identity "https://github.com/inference-gateway/cli/.github/workflows/release.yml@refs/heads/main" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  checksums.txt
```

**Successful Output:**

If verification succeeds, you should see output similar to:

```text
Verified OK
```

**Failed Verification:**

If verification fails, **do not use the binary**. This could indicate:

- The binary has been tampered with
- You downloaded from an unofficial source
- There was an error in the release process

### Step 4: Install the Verified Binary

Once both SHA256 and Cosign verification pass, install the binary:

```bash
chmod +x infer-darwin-amd64
sudo mv infer-darwin-amd64 /usr/local/bin/infer
```

---

## Platform-Specific Binary Names

Replace `infer-darwin-amd64` with your platform's binary name:

| Platform | Architecture | Binary Name |
| ---------- | ------------- | ------------- |
| macOS | Intel (amd64) | `infer-darwin-amd64` |
| macOS | Apple Silicon (arm64) | `infer-darwin-arm64` |
| Linux | amd64 | `infer-linux-amd64` |
| Linux | arm64 | `infer-linux-arm64` |

---

## Verifying Specific Versions

To verify a specific release version instead of the latest:

Replace `latest` in the download URLs with the version tag (e.g., `v0.77.0`):

```bash
# Example for version v0.77.0
curl -L -o infer-darwin-amd64 \
  https://github.com/inference-gateway/cli/releases/download/v0.77.0/infer-darwin-amd64
```

---

## Troubleshooting

### Checksum Mismatch

If the SHA256 checksums don't match:

1. **Retry the download**: The download may have been interrupted or corrupted
2. **Check your network**: Ensure you're not behind a proxy that modifies downloads
3. **Verify the source**: Ensure you're downloading from the official GitHub releases page

### Cosign Verification Fails

If Cosign verification fails:

1. **Check Cosign version**: Ensure you have a recent version of Cosign installed
2. **Verify certificate identity**: Ensure the `--certificate-identity` matches exactly
3. **Check file permissions**: Ensure all downloaded files are readable
4. **Re-download files**: The signature files may have been corrupted

### Certificate Decoding Issues

If `base64 -d` fails:

```bash
# Try alternative decoding methods
base64 --decode checksums.txt.pem > checksums.txt.pem.decoded

# Or use openssl
openssl base64 -d -in checksums.txt.pem -out checksums.txt.pem.decoded
```

---

## Security Best Practices

1. **Always verify binaries** before installation, especially in production environments
2. **Use HTTPS** when downloading to prevent man-in-the-middle attacks
3. **Pin specific versions** in automated deployments rather than using `latest`
4. **Store verification scripts** in version control for reproducible builds
5. **Verify checksums AND signatures** for maximum security (not just one method)

---

## Additional Resources

- [Cosign Documentation](https://docs.sigstore.dev/cosign/overview/)
- [Sigstore Project](https://www.sigstore.dev/)
- [Supply Chain Security Best Practices](https://slsa.dev/)
- [GitHub Releases Page](https://github.com/inference-gateway/cli/releases)

---

[← Back to README](../../README.md)
