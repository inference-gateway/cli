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
  the rooms; `config.DefaultRoomsConfig()` holds the `public` room with `agents: ["*"]`, and a
  rooms.yaml that defines `public` itself replaces it (`RoomsConfig.WithDefaultRoom`).
- `infer rooms list` and `infer rooms show` read the resolved rooms and the log through this context.
- Later steps route a post into the log (`JSONLLog.Append`), then start member sessions with
  `room-<room>-<agent>` ids and `Origin.InvokedBy = domain.InvokedByRoom` in the room's project dir.
