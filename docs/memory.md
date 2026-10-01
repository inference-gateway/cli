# Persistent Memory

[← Back to README](../README.md)

**What** - a durable, cross-session memory of individual Markdown fact-files catalogued by an auto-maintained `MEMORY.md` index.
**Why** - the agent otherwise forgets everything between sessions, and re-discovering project facts on every run wastes turns and tokens.
**How** - memory is on by default; the index is injected at session start and the agent reads or
writes individual facts through the `Memory` tool. Configure it in `memory.yaml`.

```yaml
# .infer/memory.yaml
enabled: true
dir: ""                # "" => ~/.infer/memory
max_chars: 2000        # cap on the injected MEMORY.md index
max_entry_chars: 2000  # cap on a single fact's content at write time (0 means the default)
backend:
  type: local          # local (default) keeps the directory plain | git backs it with a remote
  git:                 # read only when type is git
    repo: ""
    branch: main
    commit_message: "chore(memory): sync"
    timeout: 60        # seconds per git operation
    sync:
      on_start: pull   # pull (default) | off
      on_finish: push  # push (default) | off
```

A git-backed memory directory is pulled on run start, and committed and pushed when a fact changes,
using the ambient git credential chain (ssh-agent, credential helper, `GIT_*` environment).

Two default reminders keep memory in use. `memory-consult` points the agent at the index at session start,
and `memory-hygiene` nudges it once after 25 turns to save what a future session would otherwise miss.
Turn memory off with `memory.enabled=false` (or `INFER_MEMORY_ENABLED=false`), and both reminders are pruned automatically.

## Related

- [Tools Reference](tools-reference.md#tool-overview) - the `Memory` tool
- [Configuration Reference](configuration-reference.md#system-reminders-remindersyaml) - overriding or removing the memory reminders
- [Directory Structure](directory-structure.md) - where fact-files and the index live
