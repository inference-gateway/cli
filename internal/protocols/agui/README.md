# agui

**What** - the AG-UI protocol as a general-purpose package: a writer for the events of one run, and a localhost
WebSocket binding with both of its ends.
**Why** - external orchestrators and clients need a stable published event schema carried over a transport that
does not care who connects, and the rest of the CLI needs one place that knows the protocol.
**How** - `Run` writes one run as newline-delimited AG-UI JSON, one event per `Write`, and guarantees exactly one
terminal `RUN_FINISHED` or `RUN_ERROR`. `Binding` owns the listener, the `Origin` check, the token handshake,
the connections and their pings, and hands every frame to its `Handler`. `Dial` is the client end and hands
the frames it receives to a `Handler` too.

## What it does not know

This package names no client kind, no context and no host. Its only internal import is the logger.
Everything else imports it:

- `Run` takes text, tool calls, snapshots and CUSTOM events as plain values. Which agent event becomes which
  AG-UI event is decided by the caller.
- A `Conn` carries the client kind its hello declared as an opaque string. What a kind means is the
  handler's business. It also answers `HelloAttr` with every other field the hello declared, minus the
  token, which the binding never interprets either - the browser relay logs the extension's
  `extension_version` from there.
- The handshake frame names, the token, the port and the accepted origins come in through `BindingConfig` and
  `DialConfig`, whose `HelloAttrs` are the other hello fields a dialing client declares and the host can read
  back with `HelloAttr`. `BindingConfig.Routes` registers optional non-WebSocket HTTP handlers, e.g. static
  files, under the same loopback listener as `/ws`. They may serve files and know about clients, the binding
  does not.
- The AG-UI SDK stays confined here. `Message`, `Role`, `ToolCall` and `FunctionCall` are the types a caller
  builds a `MESSAGES_SNAPSHOT` from.

## Dependencies

The import list is closed. depguard (`agui-imports-are-closed` in `.golangci.yml`, strict mode) rejects any
import that is not recorded here, in the package and in its tests alike. Adding one means changing the rule
and this table in the same change.

| Import | Used for |
| --- | --- |
| Go standard library | everything else |
| `github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events` | the AG-UI events `Run` writes |
| `github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types` | the `MESSAGES_SNAPSHOT` message types |
| `github.com/google/uuid` | message and tool result ids |
| `github.com/gorilla/websocket` | the socket of `Binding` and `Dial` |
| `github.com/inference-gateway/cli/internal/platform/logger` | logging |
| `github.com/inference-gateway/cli/internal/protocols/agui` | tests only, the package under test |
| `github.com/inference-gateway/cli/tests/mocks/agui` | tests only, the generated `Handler` fake |

## How it plugs in

- `internal/presentation/headless` owns the mapping from agent chat events to a `Run` (`RunEncoder`) and the
  panel units a `--serve` worker answers panel requests with (`Panel`).
- `internal/computer` returns the `CustomEvent`s it publishes for its own chat events.
- `internal/browser` builds the extension relay and client on `Binding` and `Dial`.
- `cmd/daemon` hosts the one `Binding` and puts the browser relay and the `sessions` thread registry behind its
  handler.

## Related

- [AG-UI Output Format](../../../docs/ag-ui-output.md)
- [Browser Extension Bridge Protocol](../../../docs/browser-extension-protocol.md)
