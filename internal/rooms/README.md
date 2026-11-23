# rooms

**What** - the rooms bounded context: a named group of agents the user talks to in one project, the
deterministic session id each member keeps (`room-<room>-<agent>`), and the room log - one
append-only JSONL record of every post the room saw.
**Why** - the user tells one room, not one agent at a time. The room log and the member seats are the
shared data every later rooms step (routing, member sessions, member replies) reads and writes, so
they live in their own context instead of growing inside chat.
**How** - `domain/` stays pure: `Room`, `Member`, `Post` and `Route`, the session-id rule, the
`"*"`-resolution of agent lists and the `Log` port. `infrastructure/jsonl.go` is the one adapter: the
file log at `~/.infer/projects/<slug of the room's project>/rooms/<room>.jsonl`. The package root
expands the configured rooms against the agents the registry currently knows.

## How it plugs in

- `rooms.yaml` (a split sidecar like judge.yaml, merged under the `rooms` key) holds the router and
  the rooms. `config.DefaultRoomsConfig()` holds the `public` room with `agents: ["*"]`, and a
  rooms.yaml that defines `public` itself replaces it (`RoomsConfig.WithDefaultRoom`).
- `infer rooms list` and `infer rooms show` read the resolved rooms and the log through this context.
- `Router` (`router.go`) decides which members get a post. An `@x` is a mention only when `x` names
  an agent the CLI knows (`domain.Mentions`), so `@README.md` and `@./tester` stay file references.
  Mentioned members get the post outright, and a mentioned agent outside the room gets a notice. A
  post that mentions no member asks the `decision` context one predicate per member on
  `rooms.router.model`, goes to every member at or above `min_confidence`, and otherwise follows
  `fallback`: `top` sends it to the most relevant member, `none` asks the author for a mention.
  Without a model only mentions route. The routes and their probabilities land on the post.
- Later steps log a routed post (`JSONLLog.Append`), then start member sessions with
  `room-<room>-<agent>` ids, the session `Origin` recording `room`, in the room's project dir.
- Room names have no dash, so the room part of a member id ends at the first one and no two seats
  in different rooms share a session.
