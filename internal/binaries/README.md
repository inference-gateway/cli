# binaries

**What** - the binaries bounded context: keeping the prebuilt tools (`whisper-cli`, `ffmpeg`, `llama-tts`) in
`~/.infer/bin/tools` current with the [inference-gateway/binaries](https://github.com/inference-gateway/binaries) release.
**Why** - speech and computer-use share `ffmpeg`, and Desktop and opentask share the same directory, so one
owner decides when a binary is installed or replaced.
**How** - `domain/` defines the `Store` port (`Ensure`, `Install`, `Status`), and `infrastructure/` implements it
by running the release's `install.sh` and checking each binary's sha256 against `checksums.txt`.

## How it plugs in

- `Ensure` installs a missing or stale binary on first use. It checks the release at most once per process,
  and an unreachable release keeps the existing binary.
- `audio` and `computer` construct the store themselves and call `Ensure`. `audio` gates the download on
  `speech_to_text.auto_download` and `text_to_speech.auto_download`.
- `infer binaries install` and `infer binaries status` call `Install` and `Status` directly.

## Related

- [Commands Reference](../../docs/commands-reference.md#infer-binaries)
- [Speech-to-Text](../../docs/speech-to-text.md#prebuilt-binaries)
