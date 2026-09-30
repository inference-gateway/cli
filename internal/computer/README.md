# computer

**What** - the computer bounded context: reading the accessibility tree, pressing labelled controls, capturing
screenshots, moving the pointer and typing.
**Why** - desktop control needs platform-native calls, coordinate scaling and a rate limit, none of which belong in the agent loop.
**How** - `domain/` defines the accessibility and observation contracts, and `infrastructure/` provides the platform
bridges and the screenshot stream.

## How it plugs in

- `NewTools(...)` builds `Computer`, `GetLatestFrame`, `RecordStart` and `RecordStop` from the manifests in
  `tools/`, and the container registers them into the tools registry.
- The accessibility provider is macOS-only today. Other platforms return an unsupported error and the tool falls
  back to screenshot guidance, so a native failure never takes down the CLI.
- `PublishedEvent` maps this context's chat events to the run state they patch (the `screenRecording` key).
  Headless passes it to its run encoder. A pause or resume publishes nothing: pausing stops the run and
  resuming starts a new one, which clients see through the run frames.
- A file lock keeps two computer-use sessions from driving the same desktop at once. It is a no-op on Windows.

## Related

- [Computer Use](../../docs/computer-use.md)
- [Vision](../../docs/vision.md)
- [Configuration Reference](../../docs/configuration-reference.md#screen-recording-computer_useyaml)
