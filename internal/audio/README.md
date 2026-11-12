# audio

**What** - the audio capability: recording and transcribing speech with Whisper, and reading the length of a WAV.
**Why** - recording and transcribing run local binaries and model files with their own downloads and timeouts, none of which belong in the agent loop.
**How** - the container wires the recorder and transcriber into the voice shortcut, and `TextToSpeech` reports the length of the WAV the gateway returned.

## How it plugs in

- `speech_to_text` enables the voice shortcut and the transcription of Telegram voice messages. The Whisper
  model is downloaded on first use.
- Speech synthesis is not here: `TextToSpeech` goes through the agent's speech service to the gateway.
- Music, sound effects and video are not here: those tools call the gateway through the agent's media ports.

## Related

- [Speech-to-Text](../../docs/speech-to-text.md)
- [Text-to-Speech](../../docs/text-to-speech.md)
