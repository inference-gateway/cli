# github

**What** - the GitHub capability: expanding `#` issue references typed in chat, plus the `gh`/`git` helpers the scheduler reuses.
**Why** - both depend on the `gh` CLI and a GitHub remote, which may be missing, so they stay optional and degrade to a no-op.
**How** - `issues/` implements the GitHub port in `internal/agent/domain` by shelling out to `gh`. `setup/` keeps the workflow version pins for
  the scheduler's rendered workflow plus `CommandRunner`/`RealRunner`, its `gh`/`git` execution port.

## How it plugs in

- `issues/` resolves the repository's open issues and expands `#N` into the issue's title, body and recent comments.
- `setup/` keeps the version pins for the scheduler's rendered workflow and the command runner its store and poller wrap.
- Installing or updating the OpenTask workflow is the agent's job now: mention the `/opentask` skills catalog skill in a normal chat turn and
  the agent does the install itself. The CLI ships no dedicated flow for it.
- There is deliberately no built-in GitHub tool. Bash with the `gh` CLI and the `/scm` shortcuts are the supported paths.

## Related

- [Shortcuts Guide](../../docs/shortcuts-guide.md)
- [Commands Reference](../../docs/commands-reference.md)
