# Persistent Memory

Persistent memory is the agent's durable, cross-session store: individual Markdown fact-files catalogued by an auto-maintained `MEMORY.md` index.
It is **on by default**. The index is injected into every session's context at start, and the agent reads, writes and deletes individual facts
through the `Memory` tool (`operation: read` / `write` / `delete`), so a fact saved in one session is there in the next.

This example scripts both sessions with a [tokenless](https://github.com/inference-gateway/tokenless) mock model: one headless run writes the fact, and
the next run shows the index in its prompt context and reads the fact back - then deletes it and flips the feature off. The fact-files land in a
`.memory/` directory inside this example (`memory.dir` env override) and the userspace config lives in a demo HOME, so your own `~/.infer` is
untouched. You need [Task](https://taskfile.dev).

See [Persistent Memory](../../docs/memory.md) for the complete reference.

## Layout

```text
memory/
├── Taskfile.yml            # one task per session, with the store env and the mock model
├── scenarios.yaml          # what the mock model says and which tool it calls, per prompt
├── reminders.yaml          # demo: lowers the memory-hygiene nudge to once after 1 turn, so a short session can show it
├── .home/                  # demo HOME created by the tasks (removed by task clean)
└── .memory/                # the store: MEMORY.md plus the fact-files (removed by task clean)
```

## Run it

| Task | What happens |
| --- | --- |
| `task write` | A headless session where the mock model calls the `Memory` tool with `operation: write` - the fact lands in the demo store |
| `task store` | Prints the store: where the fact-file and the `MEMORY.md` index landed, with their contents |
| `task recall` | A new session: `infer debug agent system_prompt` shows the injected `PERSISTENT MEMORY INDEX`, and a `Memory` read serves the fact |
| `task reminders` | With memory on, the `memory-consult` reminder fires at session start and the `memory-hygiene` reminder fires during it |
| `task delete` | The mock model calls `Memory` with `operation: delete` - the fact-file and its index entry disappear |
| `task disabled` | With `INFER_MEMORY_ENABLED=false` the index leaves the prompt context and the memory reminders are pruned |
| `task all` | The whole story end to end: write, inspect, recall, reminders, delete, switch off |
| `task chat` | Opens `infer chat` against the demo store - say "remember ..." and reopen to see the fact |

The tasks build the repo's own binary and run it (`task build` in the repository root, no-op while up to date). To use the `infer` on your `PATH`
instead, pass it in, for example `task write INFER=infer`.

The store is pointed into this directory with `INFER_MEMORY_DIR` (the `memory.dir` setting) and every command runs with `HOME`
pointed at `.home/`, so the example never touches your real memory or config. `INFER_MEMORY_BACKEND_TYPE=local` pins the plain
directory store that users get by default - the run environment a CI agent exports can set other memory settings, and an example
should keep its own behavior.

### What to expect

`task write` - the tool result reports the fact and that it is indexed:

```text
{"tool_name":"Memory",...,"arguments":{"content":"Build with task build, then run task test. Lint with task lint and lint:imports.",
"description":"How to build and test the demo project","name":"demo-build-commands","operation":"write","type":"user"},
"success":true,...,"data":{"operation":"write","name":"demo-build-commands","type":"user",
"description":"How to build and test the demo project","path":".../examples/memory/.memory/demo-build-commands.md","size":239,"indexed":true}}
```

`task store` - one fact-file and the index, right inside the example's `.memory/`:

```text
.memory/MEMORY.md
# Memory Index

- [demo-build-commands](demo-build-commands.md) - How to build and test the demo project

.memory/demo-build-commands.md
---
name: demo-build-commands
description: How to build and test the demo project
metadata:
  type: user
  session: 7c6a540d-7130-4680-b340-8855a7fb4843
---

Build with task build, then run task test. Lint with task lint and lint:imports.
```

`task recall` - the new session's prompt context carries the index, and the `Memory` read serves the fact in full:

```text
PERSISTENT MEMORY INDEX (global facts plus facts for the current project, from past sessions; use the Memory tool 'read' with a name to load one in full):
# Memory Index

- [demo-build-commands](demo-build-commands.md) - How to build and test the demo project

{"tool_name":"Memory",...,"arguments":{"name":"demo-build-commands","operation":"read"},"success":true,
"data":{"operation":"read","name":"demo-build-commands","path":".../examples/memory/.memory/demo-build-commands.md",
"content":"---\nname: demo-build-commands\ndescription: How to build and test the demo project\n...","size":239}}
```

`task reminders` - both built-in memory reminders fired in one short session (`memory-hygiene` only nags after 25 turns by default; the demo `reminders.yaml`
lowered the threshold to 1 so a scripted session can show it):

```text
{"kind":"system_reminder","name":"memory-consult","hook":"pre_session",
"content":"<system-reminder>The persistent memory index (MEMORY.md) is already injected into your context. ...</system-reminder>"}
{"kind":"system_reminder","name":"memory-hygiene","hook":"pre_stream",
"content":"<system-reminder>Demo nudge: if this session learned something a future session would otherwise get wrong ...</system-reminder>"}
```

`task delete` - the fact is gone and the index is back to its bare header:

```text
{"tool_name":"Memory",...,"arguments":{"name":"demo-build-commands","operation":"delete"},"success":true,
"data":{"operation":"delete","name":"demo-build-commands","path":".../examples/memory/.memory/demo-build-commands.md","message":"Deleted memory \"demo-build-commands\"."}}

.memory/MEMORY.md
# Memory Index
```

`task disabled` - with the feature off, nothing memory-shaped reaches the session:

```text
no PERSISTENT MEMORY INDEX - the index only injects with memory enabled
memory-consult is pruned - the reminder set no longer mentions memory
memory-hygiene is pruned - the reminder set no longer mentions memory
```

## Global and per-project facts

A write call decides where the fact-files land: `type: user` facts are global, everything else (`feedback`, `project`, `reference`) is filed under the
detected project, and the optional `project` argument overrides it - `project: global` forces the root, any other name (`org/repo`) files it under that
project. The index lists global facts at the root and project facts under `<project>/`:

```text
.memory/                              # the store (~/.infer/memory when no dir is set)
├── MEMORY.md                         # the index, regenerated by the tool on every write and delete
├── demo-build-commands.md            # a global fact (type: user) - shown in every session
└── inference-gateway-cli/            # a project fact, from a session run inside inference-gateway/cli
    └── sandbox-gotcha.md             # shown only to sessions running in that project
```

```markdown
# Memory Index

- [demo-build-commands](demo-build-commands.md) - How to build and test the demo project
- [inference-gateway-cli/sandbox-gotcha](inference-gateway-cli/sandbox-gotcha.md) - The sandbox denies writes under ~/.infer
```

When a session starts, only global facts and the facts of the project it runs in are injected into its prompt context - other projects collapse into a
summary line, and `Memory` read still loads them by their full `<project>/<name>` form.

## The git backend

Beyond the default local store, a `memory.yaml` can version the directory with git: pull on run start, commit and push when a fact changes, using the
ambient git credential chain:

```yaml
# ~/.infer/memory.yaml
backend:
  type: git
  git:
    repo: git@github.com:your-name/memory.git
    branch: main                     # default
    commit_message: "chore(memory): sync"
    timeout: 60                      # seconds per git operation
    sync:
      on_start: pull                 # pull (default) | off
      on_finish: push                # push (default) | off
```

Run any session, and the sync happens around it with nothing extra to invoke:

```bash
infer chat
```

The directory is pulled before the first request of the session (`sync.on_start`), and every `Memory` write or delete commits and pushes after the fact
changes (`sync.on_finish`). `infer debug agent system_prompt` performs the pull too, so the prompt it prints is what a run would send.

## The switch

Memory is **enabled by default**. Turn it off and the index plus both memory reminders disappear, as `task disabled` shows:

```bash
INFER_MEMORY_ENABLED=false infer chat
```

```yaml
# ~/.infer/memory.yaml
enabled: false
```

## Without Task

The Taskfile only sets environment variables. Set them yourself to run `infer` directly from this directory:

```bash
export HOME="$PWD/.home"
export INFER_MEMORY_DIR="$PWD/.memory"
export INFER_MEMORY_BACKEND_TYPE=local
export INFER_GATEWAY_MOCK=true
export INFER_GATEWAY_MOCK_SCENARIOS="$PWD/scenarios.yaml"
export INFER_AGENT_MODEL=mock/openai/gpt-4o
export INFER_STORAGE_ENABLED=false
mkdir -p "$HOME/.infer"
cp reminders.yaml "$HOME/.infer/reminders.yaml"

infer headless --no-save "remember how to build this project" | grep '"role":"tool"'
infer headless --no-save "what does your memory say about building it" | grep '"role":"tool"'
infer debug agent system_prompt | grep 'PERSISTENT MEMORY INDEX'
```

## Use a real model

Drop `INFER_GATEWAY_MOCK`, `INFER_GATEWAY_MOCK_SCENARIOS` and `INFER_AGENT_MODEL` to talk to a real model through the gateway. Nothing else
changes - a capable model saves and recalls facts through the `Memory` tool on its own when the conversation gives it something durable, nudged
along by the memory reminders.
