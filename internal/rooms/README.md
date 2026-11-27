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
  post that pings nobody asks the `decision` context, on `rooms.router.model`, one predicate per
  member, and its routes are the members at or above `min_confidence`, best first, that it is
  offered to in turn: up to `max_members` (default 2) in a room whose `pickup` is `offer`, the
  best match alone with `top`. When nobody passes, `fallback` decides: `top` offers it to the
  most relevant members just the same, `none` asks the author for a mention. Without a model only
  pings route. The routes and their probabilities land on the post. `RouteHandover` routes a member's
  answer to the members it pings the same way, asking no model: a self-ping and a member's `@all` reach
  nobody, and a pinged outsider gets a notice the room logs as a note.
- A member's `RoomStatus` entry carries the `role` its agent file names, which clients show as
  `jeff (tester)` (`MemberStatus.Label`), and its turn names its role and the other members' roles.
- `Host` (`host.go`) is the daemon's rooms, built in `cmd/daemon` `startThreads` after the thread
  registry, with its frames a stage of `bindingHandler.Handle` before the registry. It reads its
  settings through a function the daemon backs with a fresh config load (`roomsSettings`), so a
  `rooms.yaml` edit reaches the next post, the last good settings standing in for a file that does
  not load. It answers
  `list_rooms`, `join_room`, `follow_room`, `post_room`, `clear_room`, `decide_room_approval`,
  `stop_room` and `leave_room`, logs every
  routed post, and streams posts and member activity to the followers. One lock orders the log's
  appends with the follows. `clear_room` empties the log (`Log.Clear`), tells the followers with
  `room_cleared`, and makes each member drop the posts it held and read the room from its start again.
  `join_room` follows like `follow_room` and, on the user's first visit (`domain.FirstVisit`), logs a
  `kind: join` post routed to the first `max_members` members, whose turn asks for a welcome.
  `stop_room` aborts the members at work in a room, or the one it names (`member.abort`): the member
  goes idle at once, drops the posts it held, closes a call it was waiting on and sends its thread the
  registry's `interrupt` frame, so the aborted run's later frames are no longer its turn's and log no
  answer. The room notes `<member> was stopped.` and answers `room_stopped` with who was stopped.
- `member` (`member.go`) is one seat as a client of its session's thread, the way a Telegram chat is.
  A turn resolves its room from the settings as they are now and sends `resume_conversation` with the
  agent, the room's `mode` (`auto-with-judge` unless the room says otherwise, so a judge decides the
  gated calls nobody watches) and an `Origin` recording `room`, then `set_mode` with that mode, since a
  running worker keeps the mode it started in, and the turn's activity reports it to the followers,
  then `run_agent_input` with the posts the member has not seen (`domain.Unseen`, up to
  `context_posts`), quoted as context so neither the member nor its judge takes them for the request,
  and the post for it. Every agent's mention is written without its `@` (`domain.PlainMentions`), so
  the member's file expansion never inlines a file named like a member. The run's last assistant
  message is logged as the member's post. A run that finishes carries what it spent
  (`runTokens`, `runCost`), which lands as `usage` on the post the turn writes, its answer or the note
  of a pass, as the router's decision tokens land on the user's post, without a cost. An answer goes
  through `Host.handOver`: the members it pings get it as their next turn, told who handed it over, with
  the post's `Hop`, one more than the post it answers, so a chain of hand-overs stops at
  `rooms.router.max_hops` with a note where it stopped. A run that fails
  ends on `RUN_ERROR`, which reports no spend, so its note carries none. A room's spend is
  its log's (`domain.Spent`). A room member's worker makes no next-step suggestion, which nobody reads.
  The turn tells the member to talk like a teammate in a
  channel. Only the first route of a post that pings nobody gets a turn, which may answer `PASS`
  (`domain.Room.Passes`), and `Host.passOn` then notes it and offers the post to the next route.
  The serve worker names its runs itself, so the first run that starts while a turn waits becomes
  the turn's run. A member holds new posts while it runs. A run suspends on one interrupt at a
  time, and one on a tool call is held for the room: the member posts it as `kind: approval`, and
  `decide_room_approval` makes the member resume the run with the decision (`member.decide`), the
  first decision winning, which the host logs as `kind: decision`. A run that starts while the
  call is still held was resumed from the member's own chat, and a run that ends drops the call,
  each closing the post the same way (`domain.OpenApprovals`). Any other interrupt, such as a
  question, waits in the member's own chat with a note. Idle members detach after
  `ChannelFollowTimeout`.
- Room names have no dash, so the room part of a member id ends at the first one and no two seats
  in different rooms share a session.
