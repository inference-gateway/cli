#!/usr/bin/env bash

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

GITHUB_REPO="inference-gateway/cli"
BINARY_NAME="infer"

DEFAULT_INSTALL_DIR="/usr/local/bin"
INSTALL_DIR="${INSTALL_DIR:-$DEFAULT_INSTALL_DIR}"

VERSION=""
while [[ $# -gt 0 ]]; do
    case $1 in
        --version)
            VERSION="$2"
            shift 2
            ;;
        -v|--version)
            VERSION="$2"
            shift 2
            ;;
        --install-dir)
            INSTALL_DIR="$2"
            shift 2
            ;;
        -h|--help)
            echo "Usage: $0 [OPTIONS]"
            echo "Options:"
            echo "  --version VERSION     Install specific version (e.g., v0.1.0)"
            echo "  --install-dir DIR     Installation directory (default: /usr/local/bin)"
            echo "  -h, --help            Show this help message"
            echo ""
            echo "Environment variables:"
            echo "  INSTALL_DIR           Installation directory"
            echo ""
            echo "Examples:"
            echo "  $0                             # Install latest version"
            echo "  $0 --version v0.1.0            # Install specific version"
            echo "  INSTALL_DIR=~/bin $0           # Install to custom directory"
            exit 0
            ;;
        *)
            echo -e "${RED}Error: Unknown option $1${NC}"
            exit 1
            ;;
    esac
done

print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

detect_platform() {
    local os=$(uname -s)
    local arch=$(uname -m)

    case $os in
        Linux*)
            PLATFORM="linux"
            ;;
        Darwin*)
            PLATFORM="darwin"
            ;;
        *)
            print_error "Unsupported operating system: $os"
            exit 1
            ;;
    esac

    case $arch in
        x86_64|amd64)
            ARCH="amd64"
            ;;
        arm64|aarch64)
            ARCH="arm64"
            ;;
        *)
            print_error "Unsupported architecture: $arch"
            exit 1
            ;;
    esac

    print_status "Detected platform: $PLATFORM-$ARCH"
}

check_dependencies() {
    if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
        print_error "Either curl or wget is required to download files"
        exit 1
    fi

}

download_file() {
    local url=$1
    local output=$2

    if command -v curl >/dev/null 2>&1; then
        curl -sL "$url" -o "$output"
    elif command -v wget >/dev/null 2>&1; then
        wget -q "$url" -O "$output"
    else
        print_error "Neither curl nor wget is available"
        exit 1
    fi
}

get_latest_version() {
    local api_url="https://api.github.com/repos/$GITHUB_REPO/releases/latest"

    if command -v curl >/dev/null 2>&1; then
        VERSION=$(curl -s "$api_url" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
    elif command -v wget >/dev/null 2>&1; then
        VERSION=$(wget -qO- "$api_url" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
    fi

    if [ -z "$VERSION" ]; then
        print_error "Failed to fetch latest version"
        exit 1
    fi
}

install_cli() {
    print_status "Installing $BINARY_NAME CLI..."

    detect_platform

    check_dependencies

    print_status "Installing version: $VERSION"

    FILENAME="${BINARY_NAME}-${PLATFORM}-${ARCH}"
    DOWNLOAD_URL="https://github.com/$GITHUB_REPO/releases/download/$VERSION/$FILENAME"

    print_status "Download URL: $DOWNLOAD_URL"

    TEMP_DIR=$(mktemp -d)
    trap "rm -rf $TEMP_DIR" EXIT

    print_status "Downloading $FILENAME..."
    if ! download_file "$DOWNLOAD_URL" "$TEMP_DIR/$FILENAME"; then
        print_error "Failed to download $FILENAME"
        exit 1
    fi

    if [ ! -d "$INSTALL_DIR" ]; then
        print_status "Creating installation directory: $INSTALL_DIR"
        if ! mkdir -p "$INSTALL_DIR"; then
            print_error "Failed to create installation directory: $INSTALL_DIR"
            print_warning "Try running with sudo or set INSTALL_DIR to a writable directory"
            exit 1
        fi
    fi

    print_status "Installing binary to $INSTALL_DIR/$BINARY_NAME..."
    if ! cp "$TEMP_DIR/$FILENAME" "$INSTALL_DIR/$BINARY_NAME"; then
        print_error "Failed to install binary to $INSTALL_DIR"
        print_warning "Try running with sudo or set INSTALL_DIR to a writable directory"
        exit 1
    fi

    chmod +x "$INSTALL_DIR/$BINARY_NAME"

    if [ -x "$INSTALL_DIR/$BINARY_NAME" ]; then
        print_success "$BINARY_NAME CLI installed successfully!"
        print_status "Version: $("$INSTALL_DIR/$BINARY_NAME" version 2>/dev/null || echo "Unable to verify version")"

        if ! echo "$PATH" | grep -q "$INSTALL_DIR"; then
            print_warning "$INSTALL_DIR is not in your PATH"
            print_status "Add the following line to your shell profile (~/.bashrc, ~/.zshrc, etc.):"
            echo "export PATH=\"$INSTALL_DIR:\$PATH\""
        else
            print_status "You can now run: $BINARY_NAME --help"
        fi
    else
        print_error "Installation verification failed"
        exit 1
    fi
}

# Display banner
echo -e "${BLUE}"
cat << "EOF"
 ___        __
|_ _|_ __  / _| ___ _ __ ___ _ __   ___ ___
 | || '_ \| |_ / _ \ '__/ _ \ '_ \ / __/ _ \
 | || | | |  _|  __/ | |  __/ | | | (_|  __/
|___|_| |_|_|  \___|_|  \___|_| |_|\___\___|

   ____       _
  / ___| __ _| |_ _____      ____ _ _   _
 | |  _ / _` | __/ _ \ \ /\ / / _` | | | |
 | |_| | (_| | ||  __/\ V  V / (_| | |_| |
  \____|\__,_|\__\___| \_/\_/ \__,_|\__, |
                                    |___/
EOF
echo -e "${NC}"

install_cli
