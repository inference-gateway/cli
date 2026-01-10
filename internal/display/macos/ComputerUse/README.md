# FloatingWindow.app

Native macOS floating window for Computer Use tool visualization.

## Building

The FloatingWindow.app must be built **before** building the Go binary, as it's embedded using `go:embed`.

### Local Development

```bash
cd internal/display/macos/FloatingWindow
./build.sh
```

This creates `build/FloatingWindow.app/` which is embedded in the Go binary.

### CI/CD

Add this step to your build pipeline before `go build`:

```bash
# Build FloatingWindow.app (macOS only)
if [ "$(uname)" = "Darwin" ]; then
  cd internal/display/macos/FloatingWindow
  ./build.sh
  cd -
fi

# Build Go binary (embeds FloatingWindow.app)
go build -o infer .
```

## Requirements

- macOS 10.15.4+
- Swift compiler (included with Xcode Command Line Tools)
- `swiftc` available in PATH

## Architecture

- **Source**: `main.swift` - Native NSTextView-based window
- **Config**: `Info.plist` - App metadata
- **Build**: `build.sh` - Compiles to standalone .app bundle
- **Output**: `build/FloatingWindow.app/` - Embedded in Go binary via `go:embed`

## Runtime Behavior

On first run, the embedded .app is extracted to `~/.infer/FloatingWindow.app`. Subsequent runs reuse this extracted copy.
