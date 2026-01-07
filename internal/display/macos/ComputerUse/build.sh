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

echo "Compiling Swift source for ${ARCH} (${TARGET})..."
swiftc -O \
    -sdk "$(xcrun --show-sdk-path)" \
    -target "${TARGET}" \
    -o "${MACOS_DIR}/${APP_NAME}" \
    "${SCRIPT_DIR}/main.swift"

echo "Copying Info.plist..."
cp "${SCRIPT_DIR}/Info.plist" "${CONTENTS_DIR}/Info.plist"

echo "Signing app..."
codesign --force --deep --sign - "${APP_BUNDLE}"

echo "Build complete: ${APP_BUNDLE}"
echo "App size: $(du -sh "${APP_BUNDLE}" | cut -f1)"
