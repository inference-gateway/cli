#!/usr/bin/env bash
#
# Script to calculate and update Nix package hashes
# Usage: ./nix/update-hashes.sh [VERSION]
# Example: ./nix/update-hashes.sh 0.76.1
#

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Get version from argument or extract from current infer.nix
if [ $# -eq 1 ]; then
    VERSION="$1"
else
    # Extract current version from infer.nix
    VERSION=$(grep 'version = "' nix/infer.nix | head -1 | sed 's/.*version = "\(.*\)";/\1/')
    echo -e "${YELLOW}No version specified, using current version from infer.nix: ${VERSION}${NC}"
fi

# Validate version format
if ! [[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo -e "${RED}Error: Invalid version format. Expected X.Y.Z, got: ${VERSION}${NC}"
    exit 1
fi

echo -e "${GREEN}Calculating hashes for version ${VERSION}...${NC}\n"

# Check if nix-prefetch-url is available
if ! command -v nix-prefetch-url &> /dev/null; then
    echo -e "${RED}Error: nix-prefetch-url not found. Please install Nix.${NC}"
    exit 1
fi

# 1. Calculate source hash
echo -e "${YELLOW}[1/4] Calculating source hash...${NC}"
TARBALL_URL="https://github.com/inference-gateway/cli/archive/refs/tags/v${VERSION}.tar.gz"
echo "Fetching: ${TARBALL_URL}"

SOURCE_HASH=$(nix-prefetch-url --unpack "$TARBALL_URL" 2>&1 | tail -1)

if [ -z "$SOURCE_HASH" ]; then
    echo -e "${RED}Error: Failed to calculate source hash${NC}"
    exit 1
fi

echo -e "${GREEN}Source hash: sha256-${SOURCE_HASH}${NC}\n"

# 2. Update version and source hash in infer.nix
echo -e "${YELLOW}[2/4] Updating version and source hash in infer.nix...${NC}"

# Create backup
cp nix/infer.nix nix/infer.nix.bak

# Update version
sed -i.tmp "s/version = \"[0-9.]*\";/version = \"${VERSION}\";/" nix/infer.nix

# Update source hash
sed -i.tmp "s|hash = \"sha256-[A-Za-z0-9+/=]*\";|hash = \"sha256-${SOURCE_HASH}\";|" nix/infer.nix

# Remove temp files
rm -f nix/infer.nix.tmp

echo -e "${GREEN}Updated version and source hash${NC}\n"

# 3. Calculate vendor hash
echo -e "${YELLOW}[3/4] Calculating vendor hash (this may take a minute)...${NC}"

# Set vendorHash to empty to trigger the error with correct hash
sed -i.tmp 's|vendorHash = "sha256-[A-Za-z0-9+/=]*";|vendorHash = "";|' nix/infer.nix
rm -f nix/infer.nix.tmp

# Try to build and capture the vendor hash from error
echo "Building to determine vendor hash..."
BUILD_OUTPUT=$(nix-build nix/infer.nix 2>&1 || true)

# Extract vendor hash from the error message
VENDOR_HASH=$(echo "$BUILD_OUTPUT" | grep -oP "got:\s+sha256-\K[A-Za-z0-9+/=]+" | head -1)

if [ -z "$VENDOR_HASH" ]; then
    echo -e "${RED}Error: Failed to calculate vendor hash${NC}"
    echo "Build output:"
    echo "$BUILD_OUTPUT"

    # Restore backup
    mv nix/infer.nix.bak nix/infer.nix
    exit 1
fi

echo -e "${GREEN}Vendor hash: sha256-${VENDOR_HASH}${NC}\n"

# 4. Update vendor hash in infer.nix
echo -e "${YELLOW}[4/4] Updating vendor hash in infer.nix...${NC}"

sed -i.tmp "s|vendorHash = \"[^\"]*\";|vendorHash = \"sha256-${VENDOR_HASH}\";|" nix/infer.nix
rm -f nix/infer.nix.tmp

# Remove backup if everything succeeded
rm -f nix/infer.nix.bak

echo -e "${GREEN}✓ Successfully updated all hashes!${NC}\n"

# Summary
echo "=========================================="
echo "Summary:"
echo "=========================================="
echo "Version:      ${VERSION}"
echo "Source Hash:  sha256-${SOURCE_HASH}"
echo "Vendor Hash:  sha256-${VENDOR_HASH}"
echo "=========================================="
echo ""

# Verify the build
echo -e "${YELLOW}Verifying build...${NC}"
if nix-build nix/infer.nix --show-trace; then
    echo -e "${GREEN}✓ Build successful!${NC}\n"

    # Test the binary
    echo -e "${YELLOW}Testing binary...${NC}"
    if ./result/bin/infer version; then
        echo -e "\n${GREEN}✓ Binary works correctly!${NC}\n"

        # Cleanup
        echo -e "${YELLOW}Cleaning up build artifacts...${NC}"
        rm -f result

        echo -e "${GREEN}=========================================="
        echo "All done! ✓"
        echo "==========================================${NC}"
        echo ""
        echo "Next steps:"
        echo "  1. Review changes: git diff nix/infer.nix"
        echo "  2. Commit changes: git add nix/infer.nix && git commit -m 'chore(nix): update to v${VERSION}'"
        echo "  3. Push and verify CI: git push"
        echo ""
    else
        echo -e "${RED}✗ Binary test failed${NC}"
        exit 1
    fi
else
    echo -e "${RED}✗ Build failed${NC}"
    exit 1
fi
