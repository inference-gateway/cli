# Subagents

The `Agent` tool fans work out to local subagents - each a full `infer` run in its own session - and folds their results
back into the parent conversation. This example runs a two-task fan-out offline against the
[tokenless](https://github.com/inference-gateway/tokenless) mock scripted in [`scenarios.yaml`](scenarios.yaml), with one
task dispatched to a Markdown-defined agent, so it needs no API key and no gateway. You need
[Task](https://taskfile.dev) to run the scenarios.

A Markdown agent file is one preset for the Agent tool's `agent` argument: frontmatter with `name`, `description`, an
optional `tools` allowlist and an optional `model`, and the Markdown body as the subagent's system prompt. See
[Markdown Subagents](../../docs/subagents.md) for the full file format and
[Configuration Reference](../../docs/configuration-reference.md) for the `tools.agent.*` knobs.

## Layout

```text
subagents/
├── Taskfile.yml               # one task per scenario, with the mock model's environment
├── scenarios.yaml             # what the mock model says, per prompt - parent and subagents each match their own
└── .agents/agents/
    └── researcher.md          # frontmatter: name, description, tools allowlist, model. Body: system prompt
```

The `tools` allowlist resolves against the session's real tool set, so only known tools (here `Read` and `Grep`) are
kept, and the capability follows it: every allowed tool is read-only, so the agent runs as read-only. `model: inherit`
keeps the parent's model. `tools.agent.inherit_mock` defaults to `true`, so the spawned headless subagents inherit this
example's mock gateway and run offline too.

## Run it

`task` lists the scenarios. Each headless task keeps the `"role":"tool"` line of the JSON output, which carries the
Agent tool's result - with `tools.agent.wait: true` (set in the Taskfile) that result is the fan-in and quotes every
subagent's answer.

| Task | What happens |
| --- | --- |
| `task list` | Runs `infer agents list`, which lists `researcher` next to the four built-in agents with tools, model, mode and source |
| `task fanout` | The mock model calls `Agent` with two `tasks` - one naming `researcher` through `agent`, one plain - and reads both results back |
| `task fanout:name` | The single-task shorthand: one `Agent` call with `agent: researcher` plus a `description`, no `tasks` array |
| `task all` | Runs both fan-out scenarios |
| `task chat` | Opens `infer chat` with the mock model. Ask it to fan out and watch the sub-agent list fill in under the composer |

The tasks use the `infer` on your `PATH`. To use a local build instead, run `task build` in the repository root and
pass it in, for example `task fanout INFER=../../infer`.

### What to expect

`task list`:

```text
Markdown Agents (5)
Name             Description                                     Tools                                 Model    Mode        Source
researcher       A read-only researcher that answers ...         Grep, Read                            inherit  read-only   project
explore          Read-only search agent. Use it to find ...      Grep, Read, Tree, WebFetch, WebSearch inherit  read-only   built-in
general-purpose  General-purpose agent for multi-step tasks ...  all tools (inherited)                 inherit  read-write  built-in
main             The default main agent every chat starts ...    all tools (inherited)                 inherit  read-write  built-in
plan             Read-only planning agent. Use it to design ...  Grep, Read, Tree, WebFetch, WebSearch inherit  read-only   built-in
```

Besides the example's `researcher`, which lists with source `project`, the four built-in agents that ship with the binary -
`main`, `explore`, `plan` and `general-purpose` - are appended with source `built-in`. `infer init` seeds them into
`~/.infer/agents/`, after which their source reads `user` - a Markdown file of the same name shadows a built-in in either case.

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

The first subagent ran as the `researcher` preset: its system prompt, tool allowlist and capability come from
`.agents/agents/researcher.md`. The second ran with the parent's own tools. Both keep their results to themselves until
the tool call returns - a parent and a subagent never share a session.

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

The Taskfile only sets four environment variables. Set them yourself to run `infer` directly from this directory:

```bash
export INFER_GATEWAY_MOCK=true
export INFER_GATEWAY_MOCK_SCENARIOS="$PWD/scenarios.yaml"
export INFER_AGENT_MODEL=mock/openai/gpt-4o
export INFER_TOOLS_AGENT_WAIT=true

infer agents list
infer headless --no-save --mode auto "research the repository with two subagents" | grep '"role":"tool"'
```

## Use a real model

Drop `INFER_GATEWAY_MOCK`, `INFER_GATEWAY_MOCK_SCENARIOS` and `INFER_AGENT_MODEL` to talk to a real model through the
gateway. The subagents, the Markdown agent and the knobs stay the same - only the scripted answers go away, replaced by
whatever the model decides.
