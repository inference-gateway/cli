# Telemetry

[Back to README](../README.md)

The CLI emits OpenTelemetry (OTel) traces and metrics for observability. This
document covers the telemetry configuration, the baggage keys propagated to
subprocesses and A2A agents, and the deployment considerations when upgrading
alongside the ADK.

## Baggage Keys

The CLI injects W3C Baggage members into subprocess environments (via the
`BAGGAGE` env var) and into A2A HTTP requests. These baggage keys are
env-configurable so the producer (this CLI) stays in sync with the consumer
(the ADK).

| Env var | Default | Description |
| --- | --- | --- |
| `A2A_TELEMETRY_ATTR_SESSION_ID_KEY` | `session.id` | Baggage key for the session identifier. |
| `A2A_TELEMETRY_ATTR_TOOL_CALL_ID_KEY` | `gen_ai.tool.call.id` | Baggage key for the tool call identifier. |

These env vars are read directly from the process environment via `os.Getenv`
(not through Viper / `INFER_*` config keys) because the names are a cross-repo
contract with the ADK and must match byte-for-byte. An empty value falls back
to the default.

### Mixed old/new deployment

Before this change, the CLI injected `infer.session.id` and
`infer.tool.call.id` as baggage keys. The ADK was updated in tandem to read
the new OTel-aligned keys (`session.id` / `gen_ai.tool.call.id`).

If you are upgrading a deployment where the CLI and ADK are on different
versions:

- **CLI new, ADK old**: The CLI emits `session.id` / `gen_ai.tool.call.id`
  but the old ADK still looks for `infer.session.id` /
  `infer.tool.call.id`. Set the env vars to the old names to restore
  compatibility:

  ```bash
  export A2A_TELEMETRY_ATTR_SESSION_ID_KEY=infer.session.id
  export A2A_TELEMETRY_ATTR_TOOL_CALL_ID_KEY=infer.tool.call.id
  ```

- **CLI old, ADK new**: The old CLI emits `infer.*` keys but the new ADK
  looks for `session.id` / `gen_ai.tool.call.id`. Upgrade the CLI first, or
  configure the ADK to read the old keys (see ADK docs for its equivalent
  env vars).

The recommended upgrade order is: **upgrade the CLI first, then the ADK**,
so the producer emits the new keys before the consumer expects them. During
the transition window, set the env vars above to the old names on the CLI
side.

## Span Attributes

The CLI's own span attributes are **not** affected by the baggage key env
vars. In particular, `gen_ai.conversation.id` on session and LLM-turn spans
remains hardcoded and is not unified with the baggage keys.

## OTLP Export

OTLP/HTTP export is configured via the standard OTel environment variables
(`OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_HEADERS`, etc.) or via
the CLI config (`telemetry.otlp_endpoint`, `telemetry.otlp_headers`). See
the [Configuration Reference](configuration-reference.md) for details.
