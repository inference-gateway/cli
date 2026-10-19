# github

**What** - the GitHub capability: expanding `#` issue references typed in chat, and installing the OpenTask GitHub workflow.
**Why** - both depend on the `gh` CLI and a GitHub remote, which may be missing, so they stay optional and degrade to a no-op.
**How** - `issues/` implements the GitHub port in `internal/agent/domain` by shelling out to `gh`; `setup/` keeps the
workflow version pins for the scheduler and the single install prompt the `/install-opentask` shortcut submits.

## How it plugs in

- `issues/` resolves the repository's open issues and expands `#N` into the issue's title, body and recent comments.
- `setup/` keeps the version pins for the scheduler's rendered workflow and the one install prompt
  (`InstallChatPrompt`): the `/install-opentask` shortcut submits that prompt to the chat agent as a
  regular chat turn, and the agent does the install steps itself. There is no deterministic installer.
- There is deliberately no built-in GitHub tool. Bash with the `gh` CLI and the `/scm` shortcuts are the supported paths.

## Related

- [Shortcuts Guide](../../docs/shortcuts-guide.md)
- [Commands Reference](../../docs/commands-reference.md)
