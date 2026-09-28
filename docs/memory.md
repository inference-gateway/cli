# Persistent Memory

[← Back to README](../README.md)

**What** - a durable, cross-session memory of individual Markdown fact-files catalogued by an auto-maintained `MEMORY.md` index.
**Why** - the agent otherwise forgets everything between sessions, and re-discovering project facts on every run wastes turns and tokens.
**How** - memory is on by default; the index is injected at session start and the agent reads or
writes individual facts through the `Memory` tool. Configure it in `memory.yaml`.

The agent keeps a durable, cross-session memory: individual Markdown **fact-files** under a global
directory (`~/.infer/memory` by default), catalogued by a `MEMORY.md` index. The index is injected
into context at session start, and the agent reads or writes individual facts on demand through the
`Memory` tool. A session reminder nudges it to consult and keep memory up to date.

Memory is **enabled by default**. Configure it in `memory.yaml`:

```yaml
# .infer/memory.yaml
enabled: true
dir: ""           # "" => ~/.infer/memory
max_chars: 4000   # cap on the injected MEMORY.md index
```

Turn it off with `memory.enabled=false` (or `INFER_MEMORY_ENABLED=false`); the memory-consult
reminder below is pruned automatically when memory is disabled.

## Related

- [Tools Reference](tools-reference.md) - the `Memory` tool contract
- [Reminders & Command Hooks](hooks.md) - the `memory-consult` and `memory-hygiene` reminders
- [Directory Structure](directory-structure.md) - where fact-files and the index live
