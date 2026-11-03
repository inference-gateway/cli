# Session artifacts

Browse what a session produced (generated images, recordings, downloads) from the chat TUI and save one file on
your machine, with `/artifacts` or by selecting the status bar's `⬇ artifacts` indicator.

The model is a [tokenless](https://github.com/inference-gateway/tokenless) mock scripted in
[`scenarios.yaml`](scenarios.yaml): asked for a lighthouse, it calls `ImageGeneration`, and the mock's image endpoint
answers with a canned PNG that lands under `~/.infer/projects/<project-slug>/artifacts/<session id>/`. The example
runs offline with no API key and no gateway. You need [Task](https://taskfile.dev) to run it.

## Run it

```bash
task chat
```

Then, in the chat:

1. `generate an image of a lighthouse at dusk`
2. `/artifacts` lists the session's files, newest first, with their size and modification time
3. `enter` saves the selected file under this machine's artifacts dir for the session and shows the path, `o` saves
   and opens it with the system viewer, `r` lists again, `esc` closes

`task chat:daemon` runs the same chat through `infer daemon`. Once a session teleports to another workspace (see
[session-teleport](../session-teleport/)), the view lists and reads the artifacts in that workspace through the daemon,
no `kubectl` needed.

`task headless` generates the image without the TUI and prints the tool result naming the saved path.

See [Shortcuts Guide](../../docs/shortcuts-guide.md) for `/artifacts` and the
[Daemon Binding Protocol](../../docs/browser-extension-protocol.md#session-artifacts) for the frames behind it.
