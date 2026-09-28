# Persistent Memory

[← Back to README](../README.md)

**What** - a durable, cross-session memory of individual Markdown fact-files catalogued by an auto-maintained `MEMORY.md` index.
**Why** - the agent otherwise forgets everything between sessions, and re-discovering project facts on every run wastes turns and tokens.
**How** - memory is on by default; the index is injected at session start and the agent reads or
writes individual facts through the `Memory` tool. Configure it in `memory.yaml`.

```yaml
# .infer/memory.yaml
enabled: true
dir: ""           # "" => ~/.infer/memory
max_chars: 2000   # cap on the injected MEMORY.md index
```

Two default reminders keep memory in use. `memory-consult` points the agent at the index at session start,
and `memory-hygiene` nudges it once after 25 turns to save what a future session would otherwise miss.
Turn memory off with `memory.enabled=false` (or `INFER_MEMORY_ENABLED=false`), and both reminders are pruned automatically.

## Related

- [Tools Reference](tools-reference.md#tool-overview) - the `Memory` tool
- [Configuration Reference](configuration-reference.md#system-reminders-remindersyaml) - overriding or removing the memory reminders
- [Directory Structure](directory-structure.md) - where fact-files and the index live
