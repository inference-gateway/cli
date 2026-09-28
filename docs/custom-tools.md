# Custom Tools

[← Back to README](../README.md)

Custom tools let you add tools written in any language. Drop one YAML manifest per tool into
`~/.infer/tools/` and infer offers the tool to the model next to the built-in ones. A call runs the
manifest's command with the arguments as JSON on stdin, and whatever the command prints on stdout is
the result. No SDK and no server are needed.

A custom tool behaves like a built-in tool. The model sees it under its own name, you can run it
yourself with `!!Name(arg="v")` in chat or `infer tools execute Name '{...}'`, and it follows the same
agent modes and approval flow.

For a runnable version, see [examples/custom-tools](../examples/custom-tools/): two tools, one in Python and one in
shell, driven by a scripted mock model, so it needs no API key.

## Table of Contents

- [Quick Start](#quick-start)
- [Manifest Reference](#manifest-reference)
- [How a Call Runs](#how-a-call-runs)
- [Modes and Approval](#modes-and-approval)
- [Tool Names](#tool-names)
- [Example: One Binary for Many Tools (Rust)](#example-one-binary-for-many-tools-rust)
- [Custom Tools vs. MCP](#custom-tools-vs-mcp)
- [Security](#security)
- [Using Another Directory](#using-another-directory)
- [Troubleshooting](#troubleshooting)

## Quick Start

A tool that counts the words in a file, written as a shell script.

`~/.infer/tools/WordCount.yaml`:

```yaml
name: WordCount
description: Count the words in a text file.
command:
  - ./word-count.sh
parameters:
  type: object
  properties:
    path:
      type: string
      description: Path of the file to count
  required:
    - path
modes:
  - standard
  - auto
  - auto-with-judge
  - plan
  - readonly
require_approval: false
```

`~/.infer/tools/word-count.sh` (make it executable with `chmod +x`):

```sh
#!/bin/sh
path=$(jq -r .path)
wc -w < "$path"
```

Try it without the model:

```bash
infer tools execute WordCount '{"path":"README.md"}'
```

## Manifest Reference

A custom tool manifest uses the same format as the manifests of infer's built-in tools, plus three fields that only
custom tools have: `command`, `timeout` and `enabled`. Unknown fields are rejected, so a misspelled key fails loudly
instead of silently falling back to a default.

| Field | Required | Default | Meaning |
| --- | --- | --- | --- |
| `name` | yes | | Tool name the model sees. Must match `^[A-Za-z][A-Za-z0-9_]{0,63}$` and the file name (`WordCount.yaml`). |
| `description` | yes | | What the tool does and when to use it. The model reads this to decide when to call the tool. |
| `command` | yes | | Program and fixed arguments, as a list. It runs without a shell, see below. |
| `parameters` | yes | | JSON Schema of type `object` for the call's arguments, sent to the model unchanged. |
| `modes` | no | `standard`, `auto`, `auto-with-judge` | Agent modes that offer the tool, see [Modes and Approval](#modes-and-approval). |
| `require_approval` | no | `tools.safety.require_approval` | Whether a call needs approval before it runs. |
| `timeout` | no | `30` | Seconds before the call is killed. |
| `enabled` | no | `true` | `false` keeps the manifest on disk without loading the tool. |

`command[0]` is resolved like this:

- A bare name such as `infer-desktop-tools` is looked up on `PATH`.
- A path containing a slash such as `./word-count.sh` or `bin/tool` resolves against the manifest's directory, so a tool
  can ship next to its manifest.
- An absolute path is used as is.

## How a Call Runs

1. infer checks the arguments against `parameters`: every `required` property must be present and each property must
   have its declared type. A call that fails the check never starts the process.
2. It starts `command` without a shell, in the session's working directory, with infer's environment.
3. It writes the arguments to stdin as one JSON object, for example `{"path":"README.md"}`, and closes stdin.
4. **Exit code 0**: stdout is the tool result, as text.
   **Non-zero exit code**: the call fails, and stderr (or stdout when stderr is empty) goes back to the model as the
   error.
5. The process is killed when `timeout` expires or the turn is cancelled. On Linux and macOS the whole process group
   is killed, so the children a script started die too. On Windows only the direct child is killed.

Like every tool result, the output the model sees is capped at `tools.max_result_bytes`.

## Modes and Approval

Custom tools follow the same policy as built-in tools:

- **`modes`** lists the agent modes that offer the tool. Without it the tool is offered in `standard`, `auto` and
  `auto-with-judge`, and hidden in `plan` and `readonly`, like an MCP tool. A tool that only reads can list every
  mode, as the `WordCount` example in [Quick Start](#quick-start) does. A call outside the tool's modes is refused.
- **`require_approval`** decides whether a call needs approval. Without it the tool follows the global
  `tools.safety.require_approval` (default `true`). How the approval is asked for follows
  `tools.safety.approval_behaviour` (`prompt`, `ipc`, `judge` or `block`) in both chat and headless mode, see the
  [Configuration Reference](configuration-reference.md#tool-settings).

> **Listing `readonly` declares the tool safe.** Read-only mode runs the tools it offers without asking for
> approval, so only list `readonly` (and `plan`) for tools that do not change anything.

Approval applies to calls the model makes. `!!Name(...)` in chat and `infer tools execute` are started by you and count
as approved, as they do for built-in tools. `infer tools execute --format json` still reports `approval_required`
for callers that handle approval themselves.

`tools.enabled: false` turns custom tools off together with the other local tools. Markdown subagents
(`.infer/agents/*.md`) can list custom tools in their `tools:` field.

## Tool Names

Custom tools get no prefix, so they look like built-in tools to the model. To keep that unambiguous, infer skips a
manifest whose name:

- matches the name of any built-in tool, even one your configuration switches off, compared case-insensitively
  (`read` is rejected because of `Read`), or
- starts with `MCP_`, which is reserved for MCP tools.

A custom tool never replaces a built-in tool, and nothing replaces a custom tool.

## Example: One Binary for Many Tools (Rust)

A compiled program can back several tools through subcommands, one manifest per tool.

`~/.infer/tools/TakeScreenshot.yaml`:

```yaml
name: TakeScreenshot
description: Capture a screenshot of a desktop app window and return the PNG path.
command:
  - infer-desktop-tools
  - screenshot
parameters:
  type: object
  properties:
    window:
      type: string
      description: Title of the window to capture
  required:
    - window
timeout: 60
```

`src/main.rs` (with `serde` and `serde_json` as dependencies):

```rust
use serde::Deserialize;
use std::io::Read;

#[derive(Deserialize)]
struct ScreenshotArgs {
    window: String,
}

fn screenshot(args: ScreenshotArgs) -> Result<String, String> {
    let path = format!("/tmp/{}.png", args.window.replace(' ', "_"));
    // Capture the window here.
    Ok(path)
}

fn main() {
    let mut input = String::new();
    std::io::stdin().read_to_string(&mut input).expect("reading stdin");

    let result = match std::env::args().nth(1).as_deref() {
        Some("screenshot") => serde_json::from_str(&input)
            .map_err(|e| format!("invalid arguments: {e}"))
            .and_then(screenshot),
        other => Err(format!("unknown subcommand {other:?}")),
    };

    match result {
        Ok(output) => println!("{output}"),
        Err(message) => {
            eprintln!("{message}");
            std::process::exit(1);
        }
    }
}
```

Install the binary on `PATH` (or reference it by a path relative to the manifest), and every manifest pointing at it
becomes a tool.

## Custom Tools vs. MCP

[MCP servers](mcp-integration.md) also add tools in any language. Custom tools are the lighter option when you control
the tool:

- No server to start or health-check. Nothing runs until a call is made, which keeps each `infer headless` start cheap.
- No prefix: the tool is `TakeScreenshot`, not `MCP_<server>_TakeScreenshot`.
- Per-tool `modes` and `require_approval`. MCP tools are hidden in plan mode and use the global approval setting.

`~/.infer/tools/` is yours. It is unrelated to `~/.infer/bin/tools/`, which `infer binaries` owns and fills with the
prebuilt helper programs infer itself uses (ffmpeg, whisper-cli, llama-tts).

## Security

A custom tool runs with **your** permissions and can do anything you can. infer's sandbox settings
(`tools.sandbox.directories`, `tools.sandbox.protected_paths`) only restrict infer's built-in file tools, not the
programs custom tools start. Only install manifests and programs you trust, keep `require_approval` on for tools that
change things, and list `plan`/`readonly` in `modes` only for tools that do not.

Custom tools load only from your user directory. A project's `.infer/tools/` is not read, so cloning a repository
cannot register executables.

## Using Another Directory

Set `tools.custom_dir` in `config.yaml`, or the `INFER_TOOLS_CUSTOM_DIR` environment variable, to load manifests from
another directory instead of `~/.infer/tools/`:

```bash
INFER_TOOLS_CUSTOM_DIR=/opt/my-app/tools infer headless "Take a screenshot of the editor"
```

An app that embeds infer can use this to offer its own tools without adding them to the user's terminal CLI.

## Troubleshooting

- **The tool does not show up.** infer skips an invalid manifest with a warning in its logs (`~/.infer/logs/`) and
  starts anyway. The warning names the file and the reason, such as an unknown field, a name that does not match the
  file name, or a taken name.
- **Test a tool without the model.** `infer tools execute Name '{"arg":"value"}'` runs it directly and prints the
  result or the error.
- **The call fails with "executable file not found".** A bare command name must be on the `PATH` infer runs with. Use
  a path relative to the manifest, or an absolute path, instead.
