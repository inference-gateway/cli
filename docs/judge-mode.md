# Judge Mode

[← Back to README](../README.md)

Judge mode (displayed as **Auto+Judge**) is an autonomous operating mode: there is no
human in the loop, but tool calls that would normally prompt you are decided by an
**LLM judge** instead of bypassing approval the way Auto-Accept does. One judge call
answers every gated tool call of a turn; everything else about the agent loop is
unchanged.

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
  per-tool `require_approval` - is decided by the judge. All judge-routed calls of a
  turn go to the judge in **one request** (chunks of 20 calls when a turn has more),
  so parallel tool calls wait for one judge round trip instead of one per call.
  Calls a human decides in the same turn still prompt one at a time, in tool-call
  order, after the judge has answered.
- The judge prompt carries the first and the latest non-hidden user messages (the
  intents) and every pending tool call, each as its own `<tool_call id="...">` block
  (name + arguments, capped per call).
- An approved call executes; a rejection becomes a failed tool result
  (`rejected by judge: <reason>`) and the turn continues, so the model reads the
  reason and adjusts (the mode guidance tells it to change approach rather than
  retry the same call). Only a human rejection ends the turn.

## Overriding a rejection (RequestApproval)

The judge can reject a legitimate call when it lacks context (a `git push` judged
against a bare "continue", say). Every rejection result therefore ends with a hint
that the model may call the `RequestApproval` tool to ask you directly. The tool
takes the rejected call (tool name + arguments), what the permission is needed for,
and why; the chat TUI shows the regular approval box for the rejected call with the
judge's reason and the model's justification above it.

- **Approve** arms a one-shot bypass: the model re-issues the exact same call and it
  runs without a judge call. The bypass covers that single invocation only.
  **Auto-Approve** does the same and switches the session to Auto-Accept mode, as
  it does for any approval.
- **Reject** returns the decision to the model as a normal tool result, so the turn
  continues and the model adjusts; explain in your next message if you want to
  steer it.
- Each rejected call can be escalated once; a second `RequestApproval` for the same
  call is refused, and calls the judge never rejected cannot be escalated at all.
- Headless runs have no interactive approver, so the tool returns a distinguishable
  `no_approver` result and tells the model not to retry.

The tool is advertised in Standard, Auto-Accept and Auto+Judge modes, so a human
approver can override a judge rejection there too. Plan mode does not advertise it,
having no approver to reach, and outside a judge rejection it does nothing.

## The verdict contract

The judge must answer with a JSON array holding one object per `<tool_call>` block,
named by its `id`:

```json
[
  {"id": "call_1", "decision": "approved", "reason": "installing the dependency the user asked for"},
  {"id": "call_2", "decision": "rejected", "reason": "the user did not ask for a push"}
]
```

The parser strips code fences and surrounding prose and requires `decision` to be
`approved` or `rejected`. A single `{"decision": ..., "reason": ...}` object without
an `id` is still accepted when the request carried exactly one call, so a custom
`judge.yaml` prompt written for the old one-object contract keeps working for
single calls. A call the judge omits, mislabels or gives an invalid decision gets
the `on_error` verdict (see below) while the rest of the batch keeps its verdicts;
unparseable output applies `on_error` to every call in the request.

## Configuring the judge (judge.yaml)

The judge is configured in its own file, **`judge.yaml`** (project
`./.infer/judge.yaml` overrides userspace `~/.infer/judge.yaml`; when the file is
absent the built-in defaults are used). It is the decision-sibling of `hooks.yaml` and
`reminders.yaml` - a separate file per concern.

```yaml
model: "" # "provider/model" id for judge calls; empty falls back to agent.model
gateway_url: "" # send judge calls to another gateway (e.g. real judge, mock driver); empty shares the agent's
timeout: 30 # per-call timeout in seconds
max_tokens: 2048 # response budget; reasoning models spend their thinking against it too
on_error: deny # what a failed judge call means: deny (default) or allow
system_prompt: |- # judge instructions (system message)
  You are the approver for an autonomous coding agent. ...
prompt: |- # user message template; {root_intent}, {intent} and {action} are filled in
  <root_request>
  {root_intent}
  </root_request>

  <latest_request>
  {intent}
  </latest_request>

  {action}
```

`{action}` expands to one `<tool_call id="...">` block per pending call, so the
template needs no tag of its own around it. A `judge.yaml` written by an older
`infer init` still carries the one-object prompts, which cannot name several calls:
run `infer init --overwrite` to refresh it (or edit the two prompts by hand).

Environment overrides (env wins over the file):

- `INFER_JUDGE_MODEL`, `INFER_JUDGE_GATEWAY_URL`, `INFER_JUDGE_TIMEOUT`, `INFER_JUDGE_MAX_TOKENS`,
  `INFER_JUDGE_ON_ERROR`, `INFER_JUDGE_SYSTEM_PROMPT`, `INFER_JUDGE_PROMPT`

Defaults: the agent's own model decides, calls time out after 30s, responses are
capped at 2048 tokens, and a failing judge **denies**.

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

- `--format json` emits a `judge_verdict` event per tool call (tool, decision, reason,
  turn), one per call even when the batch was decided in a single request.
- `--format ag-ui` mirrors it as an `ACTIVITY_SNAPSHOT` of type `judge_verdict` keyed
  `judge:<tool>:<turn>`.
- `--format text` prints a line for rejections.
- TUI users see the status line flash `Action rejected by judge policy (<model>): <reason>`,
  the judge model next to the `AUTO+JUDGE` mode indicator, and the model in each
  rejected tool card.
- With debug logging on, every judge request is also emitted on the hidden debug
  channel: `judge_request` (model, system prompt, rendered user prompt, number of
  calls) once per request and `judge_verdict` once per call.
- Judge token usage is added to the session totals and telemetry once per request,
  so the status bar and cost reports include it without multiplying it by the
  number of calls in the batch.

## Related

- [Configuration Reference](configuration-reference.md#judge-approval-judgeyaml)
- [Commands Reference](commands-reference.md)
- [Plan Mode](plan-mode.md)
