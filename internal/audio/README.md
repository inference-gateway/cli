# audio

**What** - the audio capability: recording and transcribing speech, synthesizing text to speech, and generating music and sound effects through the gateway.
**Why** - these features all shell out to or call media tooling with model files and timeouts, which is unrelated to the agent loop.
**How** - the package provides the engines, and the agent reaches them through media service ports in `agent/domain` and their adapters in `agent/infrastructure`.

## What it owns

- `recorder.go`, `transcriber.go`, `exec.go` - capturing audio and transcribing it with Whisper (`speech_to_text`).
- `synthesizer.go`, `ttsmodel.go`, `model.go` - text to speech, including model download and zero-shot voice cloning (`text_to_speech`).
- `convert.go`, `file.go` - format conversion and writing output files to disk.

## Why it is separate

Media features are off by default, need external binaries and model files, and fail in ways (missing
`ffmpeg`, missing model, no microphone) that must surface as actionable tool errors rather than agent
errors.

## How it plugs in

- The agent calls the media ports in `agent/domain` (`media.go`, `annotation.go`); `agent/infrastructure` holds the service adapters.
- Each feature is gated by its own config flag: `speech_to_text`, `text_to_speech`, `text_to_music`, `text_to_sfx`.
- Output is always written to disk, never played aloud.

## Related

- [Speech-to-Text](../../docs/speech-to-text.md)
- [Text-to-Speech](../../docs/text-to-speech.md)
- [Text-to-Music](../../docs/text-to-music.md)
