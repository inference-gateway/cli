# Rooms

A room is a named group of agents you talk to in one project. You post to the room, and the daemon decides
which members answer: the members you ping with `@name`, or, for a post that pings nobody, the members a
decision model finds relevant. Each member answers in its own session, and every post and answer lands in
one log per room.

Rooms run in the daemon. Start it with the [binding](daemon.md#the-binding) enabled, then post:

```bash
infer rooms post public "@main say hello to the room"
infer rooms show public
```

## Table of Contents

- [rooms.yaml](#roomsyaml)
- [Routing a post](#routing-a-post)
- [Members answering](#members-answering)
- [The room log](#the-room-log)
- [Commands](#commands)
- [Clients](#clients)

## rooms.yaml

`~/.infer/rooms.yaml` and a project's `.infer/rooms.yaml` hold the router and the rooms. `infer init` seeds
the home file. By default there is one room, `public`, with every agent of yours:

```yaml
router:
  model: ollama_cloud/clef:27b
  min_confidence: 0.5
  max_members: 2
  fallback: top
context_posts: 20
rooms:
  - name: public
    mode: auto-with-judge
    pickup: offer
    agents:
      - "*"
  - name: release
    project: /Users/me/Repositories/app
    mode: standard
    agents:
      - researcher
      - tester
      - writer
```

- **agents**: `"*"` stands for every agent of yours a session in the room's project can run as, the Markdown
  agents of its agent dirs. The built-ins (`main`, `explore`, `general-purpose`, `plan`) stay out, also once
  `infer init` seeded their files, and join a room only by name. `"*"` resolves each time the room is read,
  so a new agent file joins `public` on its own. A name no agent carries takes no seat.
- **pickup**: how a post that pings nobody finds the one member who takes it, so no two members work on the
  same request. `offer` (the default) offers it to the best match, who answers it or replies `PASS`, and a
  `PASS` offers it to the next best, up to `router.max_members` members. `top` hands it to the best match
  alone, with no way to pass it on.
- **project**: where the members' sessions run. Empty means the daemon's working directory.
- **mode**: the agent mode the members run in, `standard`, `plan`, `auto` or `auto-with-judge`. Empty means
  `auto-with-judge`, since nobody watches a member's own chat: an LLM judge decides its gated tool calls,
  and only a call it escalates waits for you. The judge is `judge.model` from `judge.yaml`, else the model
  the member runs on, see [Judge mode](judge-mode.md). `standard` puts you on every approval. A member's worker takes the mode
  when it starts, so a change reaches a busy member once its idle worker was let go.
- **name**: letters, digits and underscores. A file defining `public` replaces the default room, and one
  that leaves it out keeps it.
- **router.model**: the `provider/model` decision model asked about a post that pings nobody,
  `ollama_cloud/clef:27b` by default. Empty routes pings only. A set model starts the gateway with
  `DECISIONS_ENABLED=true`. When routing fails, such as without an `ollama_cloud` key, the post reaches nobody
  and you get a notice to ping a member.
- **router.min_confidence**: the probability a member needs to get an unpinged post.
- **router.max_members**: the most members an unpinged post is offered to, one after another and the most
  relevant first, and how many welcome you on your first visit. Pings are never capped.
- **router.fallback**: what happens to an unpinged post nobody passed. `top` offers it to the most relevant
  members anyway, as `pickup` says, so a `PASS` still reaches the next one. `none` asks you to ping one.
- **context_posts**: how many of the room's posts a member gets with its turn.

## Routing a post

1. `@name` pings an agent when `name` is an agent the project knows. A pinged member gets the post with no
   model asked. A pinged agent outside the room gets a notice instead. `@README.md` and `@./tester` stay file
   references, so a member's name wins over a file of the same name, and `@./<name>` reaches the file.
2. `@all` pings every member, also without a router model. It is the only way to reach every member at once.
3. A post that pings nobody, a greeting included, asks the decision model one question per member: should this
   member, given its description and the recent posts, act on the post? The members at or above
   `min_confidence` are ranked, and one of them takes the post, as `pickup` says. With `offer` the best match
   gets it first. A `PASS` logs `<member> passed it on - offered to <next>.` and offers it to the next, and
   when the last one passes, the room says nobody picked it up.
4. When nobody passes `min_confidence`, `fallback` decides. Without a router model, an unpinged post reaches
   nobody and you get a notice naming the members to ping.

The log records whom each post was routed to and with what probability, so a client can show why a member
answered. A decision request that fails still logs the post, routed to nobody, with the reason in a notice.

## Members answering

Every member keeps its own session, `room-<room>-<agent>`, running as its agent in the room's project. The
session records the room as what invoked it. A routed post becomes the member's next turn and carries the
room's posts the member has not seen since its last turn, up to `context_posts`. The member's answer, the last
assistant message of its run, is logged as its post. Members talk like teammates in a channel: they answer
what a post asks and keep greetings and small talk to one short line.

- Members routed the same post run in parallel.
- A post for a member that is still running waits and becomes its next turn.
- A member that needs a tool approved waits in its own chat. The room logs that it waits, and its answer lands
  once you decided there. In the default `auto-with-judge` mode only a call the judge rejected and the member
  escalated waits for you.
- The posts a member missed are quoted in its turn as context, so neither the member nor its judge reads
  another member's words as your request.
- A member on another workspace is reached through the lease relay like any session.
- The first time you enter a room from the chat or the desktop, the room logs your join and its first
  `max_members` members welcome you.

## The room log

Each room appends its posts to `~/.infer/projects/<slug of the room's project>/rooms/<room>.jsonl`, one JSON
object per line: `id`, `author` (`user`, a member, or `room` for the room's own notes), `kind` (`join` for
your first entry), `text`, `mentions`, `routes`, `usage` (what the post cost) and `time`. `usage` is the tokens
of routing a post of yours, which carry no cost, or the tokens and cost of the member's turn behind an answer
or the note of a pass. A turn that failed reports no spend, so its note carries none.

## Commands

| Command | What it does |
| --- | --- |
| `infer rooms list` | The rooms with their resolved members and session ids |
| `infer rooms show <room>` | One room's members, router settings and log |
| `infer rooms post <room> "<text>"` | Post through the daemon and print where the post went |
| `infer rooms clear <room>` | Empty the room's log, so it starts over |

`list`, `show` and `post` take `--format json`. `clear` empties the log through a running daemon, which empties
the room in every client following it, or where you run it when no daemon runs. A daemon that listens but does
not answer keeps its log, and `clear` fails. A member drops the posts it was holding, and a run already going
still posts its answer. Each member keeps its own session, so it still remembers its earlier turns, and the
next time you enter the room its members welcome you again. While the daemon runs, `list` and `show` take the rooms from it, so a room
without a `project` shows the log the daemon writes in its working directory. Without a daemon they resolve
the rooms where you run them.

## Clients

The chat TUI enters rooms from its status bar, see [Rooms in the chat](shortcuts-guide.md#rooms-in-the-chat).
The desktop and other clients list, join, follow, post to and clear rooms over the daemon binding with
`list_rooms`, `join_room`, `follow_room`, `post_room`, `clear_room` and `leave_room`. See [Rooms](browser-extension-protocol.md#rooms) in
the binding protocol.
