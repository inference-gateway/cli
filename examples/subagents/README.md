# Subagents

The `Agent` tool fans work out to local subagents - each a full `infer` run in its own session - and folds their results
back into the parent conversation. This example covers the whole Markdown agent story: a Markdown preset, a two-task
fan-out, the agents shipped inside the binary and how a project file shadows one, and a team whose members message
each other mid-run. Everything runs offline against the
[tokenless](https://github.com/inference-gateway/tokenless) mock scripted in [`scenarios.yaml`](scenarios.yaml), so it
needs no API key and no gateway. You need [Task](https://taskfile.dev) to run the scenarios.

A Markdown agent file is one preset for the Agent tool's `agent` argument: frontmatter with `name`, `description`, an
optional `tools` allowlist and an optional `model`, and the Markdown body as the subagent's system prompt. Four of
them are built into the binary (`main`, `explore`, `plan`, `general-purpose` in
[`internal/tools/builtin_agents`](../../internal/tools/builtin_agents/)): `infer init` seeds them into
`~/.infer/agents/`, and until then they resolve from the embed, so every name works in a fresh checkout. See
[Markdown Subagents](../../docs/subagents.md) for the full file format and
[Configuration Reference](../../docs/configuration-reference.md) for the `tools.agent.*` knobs.

## Layout

```text
subagents/
├── Taskfile.yml               # one task per scenario, with the mock model's environment
├── scenarios.yaml             # what the mock model says, per prompt - parent and subagents each match their own
├── explore-shadow.md          # the project explore.md the builtins task copies into .agents/agents/
└── .agents/agents/
    └── researcher.md          # frontmatter: name, description, tools allowlist, model. Body: system prompt
```

The `tools` allowlist resolves against the session's real tool set, so only known tools (here `Read` and `Grep`) are
kept, and the capability follows it: every allowed tool is read-only, so the agent runs as read-only. `model: inherit`
keeps the parent's model. `tools.agent.inherit_mock` defaults to `true`, so the spawned headless subagents inherit this
example's mock gateway and run offline too. Every task runs `infer` with `HOME` pointed at `.home/`, a demo home the
example owns, so `infer agents list` shows the built-ins with source `built-in` instead of the copies seeded into your
own `~/.infer/agents/` - `task clean` removes it.

## Run it

`task` lists the scenarios. Run `task build` first, or point `INFER` at the `infer` on your `PATH`. Each headless task
keeps the informative JSON lines of the output, which carry the Agent tool's results and the team notes - with
`tools.agent.wait: true` (set on every command) that result is the fan-in and quotes every subagent's answer.

| Task | What happens |
| --- | --- |
| `task list` | Runs `infer agents list`: `researcher` in the Markdown Agents table plus the four built-ins with source `built-in` |
| `task builtins` | The list again, the prompt sourced from `main.md`, then `explore` shadowed by a project file - the source flips to `project` |
| `task fanout` | The mock model calls `Agent` with two `tasks` - one naming `researcher` through `agent`, one plain - and reads both results back |
| `task fanout:name` | The single-task shorthand: one `Agent` call with `agent: researcher` plus a `description`, no `tasks` array |
| `task team` | The lead fans two teammates out; `beta` messages its lead mid-run with `Agent(to: "lead")` and the note shows in the output |
| `task all` | Runs `fanout`, `fanout:name` and `team` |
| `task chat` | Opens `infer chat` with the mock model. Fan out subagents, or switch the agent the chat runs as - see below |

Pass a different binary with `INFER`, for example `task fanout INFER=infer`.

### What to expect

`task list` - five presets: the example's `researcher` plus the four built-ins:

```text
Markdown Agents (5)
Name            Tools                                 Model    Mode        Source
researcher      Grep, Read                            inherit  read-only   project
explore         Grep, Read, Tree, WebFetch, WebSearch inherit  read-only   built-in
general-purpose all tools (inherited)                 inherit  read-write  built-in
main            all tools (inherited)                 inherit  read-write  built-in
plan            Grep, Read, Tree, WebFetch, WebSearch inherit  read-only   built-in
```

`task builtins` - the `agents list` above again, then the prompt a fresh session would send:

```text
Autonomous software engineering agent. Execute tasks iteratively until completion. For GitHub operations
(issues, pull requests, releases, the API), use the gh CLI via the Bash tool - there is no built-in GitHub
...
```

That body is the built-in `main.md`, so `main` is the default system prompt source - a non-empty
`prompts.agent.system_prompt` would still override it. The task then copies `explore-shadow.md` to
`.agents/agents/explore.md` and prints the list once more: `explore` now carries the shadow's description, a
`Grep, Read, Tree` allowlist and source `project`. Project files shadow built-ins, and `infer init` seeds the
built-ins into `~/.infer/agents/` so editing one is like editing your own file - the seed flips its source to `user`.

`task fanout` - the one `"role":"tool"` line, abbreviated:

```text
{"content":"{\"tool_name\":\"Agent\",\"tool_call_id\":\"call_0_0\",\"arguments\":{\"tasks\":[{\"agent\":\"researcher\", ...},
 {\"description\":\"Count the Markdown files under docs/ ...\",\"label\":\"docs-counter\"}]},\"success\":true,
 \"data\":{\"mode\":\"headless\",\"wait\":true,\"dispatched\":2,\"subagents\":[
   {\"label\":\"readme-summary\",\"result\":\"Researcher: the Quick Start is three commands that install infer,
    set the gateway URL and open the chat TUI.\",\"stats\":{...}},
   {\"label\":\"docs-counter\",\"result\":\"Counter: docs/ holds the reference guides of this repository, one document
    per feature.\",\"stats\":{...}}]}}","failed":false,"role":"tool","tool_call_id":"call_0_0", ...}
```

With `tools.agent.wait: true` the `Agent` call blocks until every subagent has finished and comes back as one fan-in
result - `mode: headless`, `wait: true`, one entry per task carrying the subagent's final assistant message.
`task fanout:name` returns the same shape with one entry, dispatched through the `description` shorthand.

`task team` - the fan-in, the message note and the lead's reply, abbreviated:

```text
{"data":{"subagents":[{"label":"alpha","result":"Alpha: docs/subagents.md is the guide that covers subagents - ..."},
 {"label":"beta","result":"Beta: the README Quick Start summary went to the lead. ...","stats":{"tools_succeeded":1,...}}],
 "dispatched":2,"wait":true}, "role":"tool", ...}
{"content":"[Message from beta]\n\nBeta: the README Quick Start covers three commands.\n\nReply with Agent(to: \"beta\", ...).","role":"user",...}
{"content":"Note read - the message from beta says the README Quick Start covers three commands: ...","role":"assistant",...}
```

The lead fans `alpha` and `beta` out in one `Agent` call; each teammate works in its own session, and because a team
exists they can message each other with the same tool: `Agent` with `to` and `message` (or `to: "lead"`, what `beta`
does - its tool result counts in `tools_succeeded`). The message reaches the lead as a `[Message from beta]` note, and
the answer to the next prompt is the lead reading it. Replace `lead` with a teammate's label and the lead relays the
message to that teammate instead.

## Switch the agent a chat runs as in `task chat`

`task chat` opens the mock-model chat in the demo HOME. The first status indicator under the composer reads
`Agent: main` - the chat runs as the built-in `main` agent. Switch it:

1. Press `down` twice to walk from the input to the indicators, then `enter` on `Agent: main` to open the agent
   dropdown (enabled by `chat.status_bar.indicators.agent`).
2. Pick `explore` and press `enter`. The conversation survives; the status indicator now reads `Agent: explore`.
3. Tell it `write a note to explore-notes.txt`. The mock model calls `Write`, and the call is refused at execution
   time because the read-only agent's allowlist is `Read, Grep, Tree, WebFetch, WebSearch`:

```text
tool not allowed: Write is not available to agent "explore" - use one of Read, Grep, Tree, WebFetch, WebSearch instead
```

4. Ask anything else and `explore` answers it - the agent's instructions, tool policy and frontmatter model all apply
   from the switch on. Switching back to `main` restores the full tool set.

Without the switch the same prompt is fine: `main` is a read-write agent, the `Write` executes, and
`/tmp/explore-notes.txt` holds the note. The switch applies in a daemon chat (`infer chat --daemon`) the same way.

## The knobs this run depends on

Set them through config or the `INFER_TOOLS_AGENT_*` environment variables (see
[Configuration Reference](../../docs/configuration-reference.md)):

| Knob | Default | What it does here |
| --- | --- | --- |
| `tools.agent.max_parallel` | 10 | Tasks past the cap are dropped, not queued. With two tasks and the default both run |
| `tools.agent.max_depth` | 1 | Subagents cannot spawn further subagents until this is raised |
| `tools.agent.idle_timeout` | 300 | Closes an interactive subagent's tmux pane after N seconds of silence. Headless subagents are unaffected |
| `tools.agent.inherit_mock` | true | Subagents inherit the mock gateway, which is why the subagents here run offline |
| `tools.agent.wait` | false | This example sets it `true` so the `Agent` call blocks for one aggregated result - see the async note below |
| `tools.agent.mode` | headless | Set to `interactive` to run subagents in tmux panes you can watch - the surface `idle_timeout` closes |

With the asynchronous default (no `wait`), the `Agent` call returns a dispatch notice at once, and each subagent's
`[Subagent Completed: ...]` notice arrives on its own with the final answer folded back - so the parent reads results
without polling, and `GetSubagentResult` fetches one you missed.

## Without Task

The tasks set four environment variables per command, run `infer` in the demo HOME, and for the `builtins` demo
drop an exported `INFER_PROMPTS_AGENT_SYSTEM_PROMPT` so the shipped prompt shows. Set them yourself to run `infer`
directly from this directory:

```bash
export INFER_GATEWAY_MOCK=true
export INFER_GATEWAY_MOCK_SCENARIOS="$PWD/scenarios.yaml"
export INFER_AGENT_MODEL=mock/openai/gpt-4o
export INFER_TOOLS_AGENT_WAIT=true

infer agents list
infer headless --no-save --mode auto "research the repository with two subagents" | grep '"role":"tool"'
infer headless --no-save --mode auto "run a team of two teammates, one of them messages you" | grep '\[Message from'
```

## Use a real model

Drop `INFER_GATEWAY_MOCK`, `INFER_GATEWAY_MOCK_SCENARIOS` and `INFER_AGENT_MODEL` to talk to a real model through the
gateway. The subagents, the Markdown agent, the built-ins, the team and the knobs stay the same - only the scripted
answers go away, replaced by whatever the model decides.
