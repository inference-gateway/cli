# decision

**What** - the decision bounded context: typed questions (`predicate`, `choice`) asked about shared
evidence and answered with native probabilities by a decision model.
**Why** - an approval gate needs a number to threshold on, not prose to parse. A generative judge writes a
verdict it may or may not justify, a decision model returns `p(approve)`, so fail-closed
semantics mean something. Keeping the contract in its own context lets other consumers (routing, triage,
answer adequacy) ask typed questions without borrowing the judge's prompt.
**How** - `domain/` holds the `Question`, `Answer` and `Decisions` value objects and the `Decider` port.
`infrastructure/gateway/` is the only adapter: it calls the gateway's Decisions API through the Go SDK, so the
cli never speaks a vendor's decision format and never holds a vendor credential. Which upstream answers is
decided by the `provider/model` prefix, exactly as for chat completions.

## How it plugs in

- `agent/loop` builds a `DecisionJudge` (judge.yaml `backend: decisions`) that implements the
  `JudgeApprover` port with one `predicate` per gated tool call and a `risk` choice, and thresholds
  `p(approve)` against `judge.min_confidence`.
- The composition root in `agent/loop` picks the judge backend. Nothing else imports this context.
- The gateway only serves `POST /v1/decisions` when started with `DECISIONS_ENABLED=true`. The supervisor
  sets it whenever the judge backend is `decisions`.
