# github

**What** - the GitHub capability: expanding `#` issue references typed in chat, and installing the OpenTask GitHub workflow.
**Why** - both depend on the `gh` CLI and a GitHub remote, which may be missing, so they stay optional and degrade to a no-op.
**How** - `issues/` and `setup/` implement the GitHub ports in `internal/agent/domain` by shelling out to `gh`.

## How it plugs in

- `issues/` resolves the repository's open issues and expands `#N` into the issue's title, body and recent comments.
- `setup/` backs the `/install-opentask` shortcut: an agent run writes `.github/workflows/tasks.yml` from the
  `opentask` skill on an install branch, then opens a pull request.
- There is deliberately no built-in GitHub tool. Bash with the `gh` CLI and the `/scm` shortcuts are the supported paths.

## Related

- [Shortcuts Guide](../../docs/shortcuts-guide.md)
- [Commands Reference](../../docs/commands-reference.md)
