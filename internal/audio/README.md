# audio

**What** - the audio capability: recording and transcribing speech with Whisper, and local text-to-speech with zero-shot voice cloning.
**Why** - both run local binaries and model files with their own downloads, timeouts and failure modes, none of which belong in the agent loop.
**How** - the container wires the recorder and transcriber into the voice shortcut, and the tools registry wraps the synthesizer in `TextToSpeech`.

## How it plugs in

- `speech_to_text` enables the voice shortcut. The Whisper model is downloaded on first use.
- `text_to_speech` enables the `TextToSpeech` tool. The local `qwen3-tts` backend synthesizes through this
  package; the default `gateway` backend goes through the agent's speech service instead.
- Music, sound effects and video are not here: those tools call the gateway through the agent's media ports.

## Related

- [Speech-to-Text](../../docs/speech-to-text.md)
- [Text-to-Speech](../../docs/text-to-speech.md)
