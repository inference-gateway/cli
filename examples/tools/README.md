# Custom tools

Add your own tools to `infer` in any language, with one YAML manifest per tool. This example ships one of each kind:

| Tool | Kind | Language | Policy |
| --- | --- | --- | --- |
| `WordCount` | User tool | Python | Read-only: offered in every mode, including plan and readonly, and never asks for approval |
| `SaveNote` | Project tool | Shell | Writes a file: hidden in plan and readonly, and always needs approval |

- **User tools** live in `~/.infer/tools/`. You installed them, so infer follows their manifest's `require_approval`.
  Here `user-tools/` plays that role through `INFER_TOOLS_CUSTOM_DIR`.
- **Project tools** live in a repository's `.infer/tools/` or `.agents/tools/` and load when you run `infer` in it.
  The repository supplies them, so every call needs approval except in `auto` mode, whatever the manifest says.

The model is a [tokenless](https://github.com/inference-gateway/tokenless) mock scripted in
[`scenarios.yaml`](scenarios.yaml), so the example runs offline with no API key and no gateway. You need
[Task](https://taskfile.dev) to run the scenarios, and `python3` for `WordCount`.

See [Custom Tools](../../docs/custom-tools.md) for the full manifest reference.

## Layout

```text
tools/
├── Taskfile.yml          # one task per scenario, with the mock model's environment
├── scenarios.yaml        # what the mock model says, per prompt
├── user-tools/           # user tools, standing in for ~/.infer/tools via INFER_TOOLS_CUSTOM_DIR
│   ├── WordCount.yaml    # manifest: name, description, command, parameters, modes, require_approval
│   └── word_count.py     # reads {"path": ...} on stdin, prints the counts
└── .agents/tools/        # project tools, loaded because infer runs in this directory
    ├── SaveNote.yaml
    └── save-note.sh      # appends the JSON arguments to notes.jsonl
```

A call runs the manifest's `command` without a shell, in the working directory, and writes the arguments to stdin as
one JSON object. Exit code 0 means stdout is the result. A non-zero exit sends stderr back to the model as the error.

## Run it

`task` lists the scenarios. Each one runs `infer headless` against the mock model and keeps only the `"role":"tool"`
line of its JSON output, which carries the tool's result.

| Task | What happens |
| --- | --- |
| `task word-count` | The model calls `WordCount`, which runs straight away because its manifest sets `require_approval: false` |
| `task save-note` | The model calls `SaveNote`, a project tool, which needs approval. Headless mode has no one to ask, so the call is blocked |
| `task save-note:auto` | In `auto` mode calls run without approval, so the note is saved to `notes.jsonl` |
| `task save-note:plan` | Plan mode offers only tools that list `plan` in their `modes`, so `SaveNote` is refused |
| `task all` | Runs the four scenarios above |
| `task execute` | Runs both tools yourself with `infer tools execute`, without the model |
| `task chat` | Opens `infer chat` with the mock model. Type `!!WordCount(path="README.md")` to run a tool yourself |
| `task clean` | Removes `notes.jsonl` |

The tasks use the `infer` on your `PATH`. To use a local build instead, run `task build` in the repository root and
pass it in, for example `task word-count INFER=../../infer`.

### What to expect

`task word-count`:

```text
{"content":"{\"tool_name\":\"WordCount\", ... \"data\":\"README.md: <lines> lines, <words> words, <characters> characters\\n\"}", ...}
```

`task save-note`, blocked and no note written:

```text
{"content":"Blocked: SaveNote requires approval, but approvals are not available in this session ...", ...}
```

`task save-note:auto`, then the saved note:

```text
{"content":"{\"tool_name\":\"SaveNote\", ... \"success\":true, ... \"data\":\"Saved the note to notes.jsonl.\\n\"}", ...}
{"text":"Buy milk"}
```

`task save-note:plan`:

```text
{"content":"tool not allowed: SaveNote is disabled in plan mode (read-only) ...", ...}
```

## Without Task

The Taskfile only sets four environment variables. Set them yourself to run `infer` directly from this directory:

```bash
export INFER_GATEWAY_MOCK=true
export INFER_GATEWAY_MOCK_SCENARIOS="$PWD/scenarios.yaml"
export INFER_AGENT_MODEL=mock/openai/gpt-4o
export INFER_TOOLS_CUSTOM_DIR="$PWD/user-tools"

infer headless --no-save "count the words in README.md" | grep '"role":"tool"'
infer tools execute SaveNote '{"text":"Call the plumber"}'
```

## Use a real model

Drop `INFER_GATEWAY_MOCK`, `INFER_GATEWAY_MOCK_SCENARIOS` and `INFER_AGENT_MODEL` to talk to a real model through the
gateway. The tools stay the same. To use `WordCount` in every session, copy `user-tools/` into `~/.infer/tools/`, the
default directory, instead of setting `INFER_TOOLS_CUSTOM_DIR`. `SaveNote` keeps loading from `.agents/tools/` whenever
you run `infer` in this directory.
