# Custom tools

Add your own tools to `infer` in any language, with one YAML manifest per tool. This example ships two:

| Tool | Language | Policy |
| --- | --- | --- |
| `WordCount` | Python | Read-only: offered in every mode, including plan and readonly, and never asks for approval |
| `SaveNote` | Shell | Writes a file: hidden in plan and readonly, and needs approval |

The model is a [tokenless](https://github.com/inference-gateway/tokenless) mock scripted in
[`scenarios.yaml`](scenarios.yaml), so the example runs offline with no API key and no gateway. Only
`python3` is needed for `WordCount`.

See [Custom Tools](../../docs/custom-tools.md) for the full manifest reference.

## Layout

```text
custom-tools/
├── scenarios.yaml        # what the mock model says, per prompt
└── tools/                # INFER_TOOLS_CUSTOM_DIR
    ├── WordCount.yaml    # manifest: name, description, command, parameters, modes, require_approval
    ├── word_count.py     # reads {"path": ...} on stdin, prints the counts
    ├── SaveNote.yaml
    └── save-note.sh      # appends the JSON arguments to notes.jsonl
```

A call runs the manifest's `command` without a shell, in the working directory, and writes the arguments to stdin as
one JSON object. Exit code 0 means stdout is the result. A non-zero exit sends stderr back to the model as the error.

## Run it

From this directory:

```bash
export INFER_GATEWAY_MOCK=true
export INFER_GATEWAY_MOCK_SCENARIOS="$PWD/scenarios.yaml"
export INFER_AGENT_MODEL=mock/openai/gpt-4o
export INFER_TOOLS_CUSTOM_DIR="$PWD/tools"
```

`infer headless` prints one JSON line per message. The tool's result is on the `"role":"tool"` line, so each command
below keeps only that line.

The model calls `WordCount`, which runs straight away because its manifest sets `require_approval: false`:

```bash
infer headless --no-save "count the words in README.md" | grep '"role":"tool"'
```

```text
{"content":"{\"tool_name\":\"WordCount\", ... \"data\":\"README.md: <lines> lines, <words> words, <characters> characters\\n\"}", ...}
```

The model calls `SaveNote`, which needs approval. Headless mode has no one to ask, so the call is blocked and no note
is written:

```bash
infer headless --no-save "save a note to buy milk" | grep '"role":"tool"'
```

```text
{"content":"Blocked: SaveNote requires approval, but approvals are not available in this session ...", ...}
```

In `auto` mode calls run without approval, so the note is saved:

```bash
infer headless --no-save --mode auto "save a note to buy milk" | grep '"role":"tool"'
cat notes.jsonl
```

```text
{"content":"{\"tool_name\":\"SaveNote\", ... \"success\":true, ... \"data\":\"Saved the note to notes.jsonl.\\n\"}", ...}
{"text":"Buy milk"}
```

Plan mode offers only read-only tools. `WordCount` lists `plan` in its `modes` and `SaveNote` does not, so the call is
refused:

```bash
infer headless --no-save --mode plan "save a note to buy milk" | grep '"role":"tool"'
```

```text
{"content":"tool not allowed: SaveNote is disabled in plan mode (read-only) ...", ...}
```

## Run a tool yourself

Custom tools run like built-in ones, without the model:

```bash
infer tools execute WordCount '{"path":"README.md"}'
infer tools execute SaveNote '{"text":"Call the plumber"}'
```

In `infer chat` the same calls are `!!WordCount(path="README.md")` and `!!SaveNote(text="Call the plumber")`, and both
appear in the `!!` autocomplete.

## Use a real model

Unset `INFER_GATEWAY_MOCK` and `INFER_GATEWAY_MOCK_SCENARIOS` to talk to a real model through the gateway. The tools
stay the same. To use them in every session, copy the manifests and scripts into `~/.infer/tools/`, the default
directory, instead of setting `INFER_TOOLS_CUSTOM_DIR`.
