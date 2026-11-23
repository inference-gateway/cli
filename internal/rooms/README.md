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
- `infer rooms list` and `infer rooms show` read the resolved rooms and the log through this context,
  each room resolved against the agents of its own project (`AgentCatalog`, `tools.ProjectAgents` in
  the daemon and the commands). `infer rooms post` posts through the daemon.
- `Router` (`router.go`) decides which members get a post. An `@x` is a mention only when `x` names
  an agent the CLI knows (`domain.Mentions`), so `@README.md` and `@./tester` stay file references.
  A post that pings agents asks no model: pinged members get it outright, `@all`
  (`domain.MentionsEveryone`) every member, and a pinged agent outside the room gets a notice. A
  post that pings nobody asks the `decision` context, on `rooms.router.model`, whether it is for
  the whole room (a predicate named `*`, which no agent can be named) and one predicate per
  member. A post for the whole room goes to every member, any other to at most `max_members`
  (default 2) of the most relevant members at or above `min_confidence`, so a busy room stays quiet. When nobody
  passes, `fallback` decides: `top` sends it to the most relevant member, `none` asks the author
  for a mention. Without a model only pings route. The routes and their probabilities land on the
  post.
- `Host` (`host.go`) is the daemon's rooms, built in `cmd/daemon` `startThreads` after the thread
  registry, with its frames a stage of `bindingHandler.Handle` before the registry. It answers
  `list_rooms`, `join_room`, `follow_room`, `post_room` and `leave_room`, logs every routed post, and
  streams posts and member activity to the followers. One lock orders the log's appends with the follows.
  `join_room` follows like `follow_room` and, on the user's first visit (`domain.FirstVisit`), logs a
  `kind: join` post routed to the first `max_members` members, whose turn asks for a welcome.
- `member` (`member.go`) is one seat as a client of its session's thread, the way a Telegram chat is.
  A turn sends `resume_conversation` with the agent and an `Origin` recording `room`, then
  `run_agent_input` with the posts the member has not seen (`domain.Unseen`, up to `context_posts`)
  and the post for it, every agent's mention written without its `@` (`domain.PlainMentions`) so the
  member's file expansion never inlines a file named like a member. The run's last assistant message
  is logged as the member's post. The turn tells the member to talk like a teammate in a channel. The
  serve worker names its runs itself, so the first run that starts while a turn waits becomes the turn's
  run. A member holds new posts while it runs, and a run that ends on an
  interrupt leaves the approval to the member's own chat and logs a note. Idle members detach after
  `ChannelFollowTimeout`.
- Room names have no dash, so the room part of a member id ends at the first one and no two seats
  in different rooms share a session.
