#!/bin/bash

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
APP_NAME="ComputerUse"
BUILD_DIR="${SCRIPT_DIR}/build"
APP_BUNDLE="${BUILD_DIR}/${APP_NAME}.app"
CONTENTS_DIR="${APP_BUNDLE}/Contents"
MACOS_DIR="${CONTENTS_DIR}/MacOS"
RESOURCES_DIR="${CONTENTS_DIR}/Resources"

echo "Building ${APP_NAME}.app..."

rm -rf "${BUILD_DIR}"

mkdir -p "${MACOS_DIR}"
mkdir -p "${RESOURCES_DIR}"

ARCH=$(uname -m)
if [ "$ARCH" = "arm64" ]; then
    TARGET="arm64-apple-macos11.0"
else
    TARGET="x86_64-apple-macos10.15.4"
fi

# Resolve SDK path. Prefer SDKROOT (set by Nix darwin builds via apple-sdk),
# fall back to xcrun on systems with Xcode Command Line Tools installed.
if [ -n "${SDKROOT:-}" ]; then
    SDK_PATH="${SDKROOT}"
elif command -v xcrun >/dev/null 2>&1; then
    SDK_PATH="$(xcrun --show-sdk-path)"
else
    echo "Error: Cannot locate macOS SDK. Set SDKROOT or install Xcode Command Line Tools." >&2
    exit 1
fi

echo "Compiling Swift source for ${ARCH} (${TARGET}) using SDK ${SDK_PATH}..."
swiftc -O \
    -sdk "${SDK_PATH}" \
    -target "${TARGET}" \
    -o "${MACOS_DIR}/${APP_NAME}" \
    "${SCRIPT_DIR}/Models/Events.swift" \
    "${SCRIPT_DIR}/EventSystem/EventDispatcher.swift" \
    "${SCRIPT_DIR}/EventSystem/EventReader.swift" \
    "${SCRIPT_DIR}/ViewModels/MainViewModel.swift" \
    "${SCRIPT_DIR}/Utilities/Design.swift" \
    "${SCRIPT_DIR}/Utilities/OutputWriter.swift" \
    "${SCRIPT_DIR}/Coordinators/WindowCoordinator.swift" \
    "${SCRIPT_DIR}/Views/ClickIndicator.swift" \
    "${SCRIPT_DIR}/Views/MoveTrail.swift" \
    "${SCRIPT_DIR}/Views/ControlBar.swift" \
    "${SCRIPT_DIR}/Views/ImageThumbnail.swift" \
    "${SCRIPT_DIR}/Views/MainWindow.swift" \
    "${SCRIPT_DIR}/main.swift"

echo "Copying Info.plist..."
cp "${SCRIPT_DIR}/Info.plist" "${CONTENTS_DIR}/Info.plist"

if command -v codesign >/dev/null 2>&1; then
    echo "Signing app..."
    codesign --force --deep --sign - "${APP_BUNDLE}"
else
    echo "Skipping codesign (codesign not available in this environment)"
fi

echo "Build complete: ${APP_BUNDLE}"
echo "App size: $(du -sh "${APP_BUNDLE}" | cut -f1)"
