# computer

**What** - the computer bounded context: reading the accessibility tree, pressing labelled controls, capturing screenshots, moving the pointer and typing.
**Why** - desktop control needs platform-native calls, coordinate scaling and a rate limit, none of which belong in the agent loop.
**How** - `domain/` defines the accessibility and observation contracts, and `infrastructure/` provides the platform bridges and the screenshot stream.

## What it owns

- `domain/accessibility.go`, `domain/action.go`, `domain/observation.go`, `domain/target.go` - the provider contract and the value types it exchanges.
- `executor.go`, `coordinate_scaler.go` - executing an action and mapping coordinates between the screen and the screenshot.
- `get_latest_frame.go`, `recorder.go`, `record_tools.go`, `screen_lock.go` - frame reads, screen recording and the lock that serialises them.
- `tool.go`, `tools.go`, `tool_manifests.go` - building and describing the computer tools.
- `approval.go` - the per-action approval policy.
- `tools/*.yaml` - manifests for `Computer`, `GetLatestFrame`, `RecordStart`, `RecordStop`.

## Why it is separate

The macOS accessibility bridge, Linux AT-SPI and Windows UIA are provider implementations, not agent
logic. A native failure must degrade to screenshot guidance instead of taking down the CLI, which
needs an isolation boundary.

## How it plugs in

- `NewTools(...)` builds the tools from the manifests; the container registers them into the tools registry.
- Configure it in `computer_use.yaml` (or `INFER_COMPUTER_USE_*`). Actions are governed by `computer_use.enabled`, `rate_limit` and `approval`.
- Depguard keeps robotgo and the platform bridges inside this package.

## Related

- [Computer Use](../../docs/computer-use.md)
- [Vision](../../docs/vision.md)
- [Configuration Reference](../../docs/configuration-reference.md#screen-recording-computer_use_yaml)
