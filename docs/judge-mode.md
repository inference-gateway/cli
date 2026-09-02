# Judge Mode

[← Back to README](../README.md)

Judge mode (displayed as **Auto+Judge**) is an autonomous operating mode: there is no
human in the loop, but tool calls that would normally prompt you are decided by an
**LLM judge** instead of bypassing approval the way Auto-Accept does. One judge call
answers one pending tool call; everything else about the agent loop is unchanged.

It exists for unattended runs where a human approval prompt would deadlock (CI,
headless agents, heartbeat jobs) but you still want a gate on mutating or dangerous
actions.

## Why use it

- CI and headless agents: the default headless behaviour **blocks** any action that
  needs approval when no approver is reachable. Judge mode turns that dead end into a
  decision.
- A middle ground between Auto-Accept (no gate at all) and Standard (a human on every
  gate).
- Auditable autonomy: every verdict is published as an event, including the judge's
  reason for a rejection.

## How to enter judge mode

### Chat TUI

Press **Shift+Tab** to cycle the agent mode until the status line reads **Auto+Judge**:

```text
Standard → Plan Mode → Auto-Accept → Auto+Judge → Standard → …
```

### Headless

```bash
infer headless --mode auto-with-judge "fix issue #42"
INFER_AGENT_MODE=auto-with-judge infer headless "fix issue #42"
```

The mode key is `auto-with-judge` (also accepted by `INFER_AGENT_MODE` and the browser
extension bridge). When the judge is selected but no model can be resolved, startup
fails fast with an explanation instead of failing later per call.

## What the judge decides

The standard approval policy still decides *which* calls are gated - judge mode
changes only *who answers the gate*:

- Bash commands on the active allow-list pass for free (judge mode uses the `standard`
  list, same as Standard mode). They never reach the judge.
- Anything else that would prompt a human - off-list commands, Write/Edit/Delete,
  per-tool `require_approval` - gets exactly one judge call.
- The judge prompt carries the latest non-hidden user message (the intent) and the
  pending tool call (name + arguments).
- An approved call executes; a rejection flows through the standard rejection path, so
  the model sees a rejection tool result ending with `Rejection reason: <judge reason>`
  and is told to change the approach instead of retrying the same call.

## The verdict contract

The judge must answer with exactly one JSON object:

```json
{"decision": "approved", "reason": "installing the dependency the user asked for"}
```

The parser strips code fences and surrounding prose, requires `decision` to be
`approved` or `rejected`, and treats anything else as a failed judge call (see
`on_error` below).

## Configuring the judge (judge.yaml)

The judge is configured in its own file, **`judge.yaml`** (project
`./.infer/judge.yaml` overrides userspace `~/.infer/judge.yaml`; when the file is
absent the built-in defaults are used). It is the decision-sibling of `hooks.yaml` and
`reminders.yaml` - a separate file per concern.

```yaml
model: "" # "provider/model" id for judge calls; empty falls back to agent.model
timeout: 30 # per-call timeout in seconds
max_tokens: 256 # response budget - the verdict is a tiny JSON object
on_error: deny # what a failed judge call means: deny (default) or allow
system_prompt: |- # judge instructions (system message)
  You are the approver for an autonomous coding agent. ...
prompt: |- # user message template; {intent} and {action} are filled in
  <user_request>
  {intent}
  </user_request>

  <tool_call>
  {action}
  </tool_call>
```

Environment overrides (env wins over the file):

- `INFER_JUDGE_MODEL`, `INFER_JUDGE_TIMEOUT`, `INFER_JUDGE_MAX_TOKENS`,
  `INFER_JUDGE_ON_ERROR`, `INFER_JUDGE_SYSTEM_PROMPT`, `INFER_JUDGE_PROMPT`

Defaults: the agent's own model decides, calls time out after 30s, responses are
capped at 256 tokens, and a failing judge **denies**.

**`on_error` semantics.** A judge call can fail (timeout, gateway error, unparseable
output) or return garbage. `on_error: deny` (default) rejects the call with a
distinguishable `judge unavailable: ...` reason - the same fail-closed default as the
no-approver block path. `on_error: allow` approves instead; choose it only if the
judge is a convenience and availability matters more than the gate.

**Model resolution.** `judge.model` falls back to `agent.model` (same precedent as
conversation title generation). Selecting the judge with neither set is a
configuration error caught at startup.

## Judge as the approval behaviour, without the mode

You can route gated calls to the judge in **any** mode with
`tools.safety.approval_behaviour: judge` (env: `INFER_TOOLS_SAFETY_APPROVAL_BEHAVIOUR`).
This is useful when you want a judge gate in Standard mode or under the channel
manager: the judge is always reachable (headless and CI included), so unlike `ipc` it
is never downgraded to block. Mode selection and behaviour selection compose - the
auto-with-judge mode forces the judge regardless of `approval_behaviour`.

## Observability

- `--format json` emits a `judge_verdict` event per decision (tool, decision, reason,
  turn).
- `--format ag-ui` mirrors it as a custom event.
- `--format text` prints a line for rejections.
- TUI users see the status line flash `Action rejected by judge policy: <reason>`.
- With debug logging on, every judge call is also emitted on the hidden debug
  channel: `judge_request` (model, system prompt, rendered user prompt) and
  `judge_verdict`.
- Judge token usage is added to the session totals and telemetry, so the status
  bar and cost reports include it.

## Related

- [Configuration Reference](configuration-reference.md#judge-approval-judgeyaml)
- [Commands Reference](commands-reference.md)
- [Plan Mode](plan-mode.md)
