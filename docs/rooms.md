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
the home file. By default there is one room, `public`, with every agent:

```yaml
router:
  model: ""
  min_confidence: 0.5
  max_members: 2
  fallback: top
context_posts: 20
rooms:
  - name: public
    agents:
      - "*"
  - name: release
    project: /Users/me/Repositories/app
    agents:
      - researcher
      - tester
      - writer
```

- **agents**: `"*"` stands for every agent a session in the room's project can run as, the Markdown agents
  of its agent dirs and the built-ins (`main`, `explore`, `general-purpose`, `plan`). It resolves each time
  the room is read, so a new agent file joins `public` on its own. A name no agent carries takes no seat.
- **project**: where the members' sessions run. Empty means the daemon's working directory.
- **name**: letters, digits and underscores. A file defining `public` replaces the default room, and one
  that leaves it out keeps it.
- **router.model**: the `provider/model` decision model asked about a post that pings nobody. Empty routes
  pings only. Setting it starts the gateway with `DECISIONS_ENABLED=true`.
- **router.min_confidence**: the probability a member needs to get an unpinged post.
- **router.max_members**: the most members that answer an unpinged post, the most relevant first, so a busy
  room does not get five answers to one question. Pings are never capped.
- **router.fallback**: what happens to an unpinged post nobody passed. `top` sends it to the most relevant
  member, `none` asks you to ping one.
- **context_posts**: how many of the room's posts a member gets with its turn.

## Routing a post

1. `@name` pings an agent when `name` is an agent the project knows. A pinged member gets the post with no
   model asked. A pinged agent outside the room gets a notice instead. `@README.md` and `@./tester` stay file
   references, so a member's name wins over a file of the same name, and `@./<name>` reaches the file.
2. A post that pings nobody asks the decision model one question per member: should this member, given its
   description and the recent posts, act on the post? The post goes to at most `max_members` members at or
   above `min_confidence`.
3. When nobody passes, `fallback` decides. Without a router model, an unpinged post reaches nobody and you get
   a notice naming the members to ping.

The log records whom each post was routed to and with what probability, so a client can show why a member
answered. A decision request that fails still logs the post, routed to nobody, with the reason in a notice.

## Members answering

Every member keeps its own session, `room-<room>-<agent>`, running as its agent in the room's project. The
session records the room as what invoked it. A routed post becomes the member's next turn and carries the
room's posts the member has not seen since its last turn, up to `context_posts`. The member's answer, the last
assistant message of its run, is logged as its post.

- Members routed the same post run in parallel.
- A post for a member that is still running waits and becomes its next turn.
- A member that needs a tool approved waits in its own chat. The room logs that it waits, and its answer lands
  once you decided there.
- A member on another workspace is reached through the lease relay like any session.

## The room log

Each room appends its posts to `~/.infer/projects/<slug of the room's project>/rooms/<room>.jsonl`, one JSON
object per line: `id`, `author` (`user`, a member, or `room` for the room's own notes), `text`, `mentions`,
`routes` and `time`.

## Commands

| Command | What it does |
| --- | --- |
| `infer rooms list` | The rooms with their resolved members and session ids |
| `infer rooms show <room>` | One room's members, router settings and log |
| `infer rooms post <room> "<text>"` | Post through the daemon and print where the post went |

All three take `--format json`.

## Clients

The desktop and other clients list, follow and post to rooms over the daemon binding with `list_rooms`,
`follow_room`, `post_room` and `leave_room`. See [Rooms](browser-extension-protocol.md#rooms) in the binding
protocol.
