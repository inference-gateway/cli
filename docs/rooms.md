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
  max_hops: 3
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
  the member runs on, see [Judge mode](judge-mode.md). `standard` puts you on every approval. The room's mode
  wins over the mode of the chat you post from: inside a room the chat's mode indicator shows it, and
  shift+tab does not change it.
- **name**: letters, digits and underscores. A file defining `public` replaces the default room, and one
  that leaves it out keeps it.
- **router.model**: the `provider/model` decision model asked about a post that pings nobody,
  `ollama_cloud/clef:27b` by default. Empty routes pings only. A set model starts the gateway with
  `DECISIONS_ENABLED=true`. When routing fails, such as without an `ollama_cloud` key, the post reaches nobody
  and you get a notice to ping a member.
- **router.min_confidence**: the probability a member needs to get an unpinged post.
- **router.max_members**: the most members an unpinged post is offered to, one after another and the most
  relevant first, and how many welcome you on your first visit. Pings are never capped.
- **router.max_hops**: how many times members may hand a post on to each other with a ping before it stops,
  3 by default, so two members cannot ping each other forever.
- **router.fallback**: what happens to an unpinged post nobody passed. `top` offers it to the most relevant
  members anyway, as `pickup` says, so a `PASS` still reaches the next one. `none` asks you to ping one.
- **context_posts**: how many of the room's posts a member gets with its turn.

The daemon reads `rooms.yaml` again each time a room is used, so an edit, such as a room's `mode`, reaches the
next post without a restart. Every member turn switches the member's worker to the room's mode first, so a
worker that started under the old mode follows too. A `rooms.yaml` that does not load leaves the rooms as they
were last read.

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
5. A member's answer that pings another member hands the post over: the pinged member gets it as its next
   turn, told who handed it over, and no model is asked. A member pinging itself, a pinged agent outside the
   room, which the room notes, and a member's `@all` reach nobody. A chain of hand-overs stops after
   `router.max_hops`, and the room notes where it stopped.

The log records whom each post was routed to and with what probability, so a client can show why a member
answered. A decision request that fails still logs the post, routed to nobody, with the reason in a notice.

## Members answering

Every member keeps its own session, `room-<room>-<agent>`, running as its agent in the room's project. The
session records the room as what invoked it. A routed post becomes the member's next turn and carries the
room's posts the member has not seen since its last turn, up to `context_posts`. The member's answer, the last
assistant message of its run, is logged as its post. Members talk like teammates in a channel: they answer
what a post asks and keep greetings and small talk to one short line. A member hands a post to another
member with `@name` when that member should act on it, the way you ping one, and never for small talk. A
member offered a post that hands it over this way takes it: the offer does not move on to the next match too.

- Members routed the same post run in parallel.
- A post for a member that is still running waits and becomes its next turn.
- A member that needs a tool call approved asks in the room. In the default `auto-with-judge` mode that is only
  a call the judge rejected and the member escalated. The room shows the call with the judge's reason and the
  member's justification, and the chat opens it in the approval box every tool approval uses, naming the member
  and the room, with **Approve** and **Reject** but no **Auto-Approve**. The first decision wins, from the room or from the
  member's own chat, session `room-<room>-<agent>`. The room logs who decided, and a later decision is refused.
  The call waits until someone decides it, however long that takes, and the member's next posts wait behind it.
  A member that asks you a question still waits in its own chat, and the room notes it.
- The posts a member missed are quoted in its turn as context, so neither the member nor its judge reads
  another member's words as your request.
- A member on another workspace is reached through the lease relay like any session.
- The first time you enter a room from the chat or the desktop, the room logs your join and its first
  `max_members` members welcome you.

## The room log

Each room appends its posts to `~/.infer/projects/<slug of the room's project>/rooms/<room>.jsonl`, one JSON
object per line: `id`, `author` (`user`, a member, or `room` for the room's own notes), `kind`, `text`,
`mentions`, `routes`, `hop`, `approval`, `usage` (what the post cost) and `time`. `kind` is `join` for your first
entry, `approval` for a member's call waiting for you and `decision` for the room's line that closes one, and
absent for a message. `approval` names the call those two posts are about: its `id`, `tool`, `arguments` and
the `decision`, `approve` or `reject`, absent when it was decided outside the room. `hop` is how many member
turns lie between your post and a member's, 1 for a direct answer, the count `max_hops` caps. `usage` is the tokens
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
The desktop and other clients list, join, follow, post to and clear rooms and decide a member's call over the
daemon binding with `list_rooms`, `join_room`, `follow_room`, `post_room`, `clear_room`, `decide_room_approval`
and `leave_room`. See [Rooms](browser-extension-protocol.md#rooms) in the binding protocol.
