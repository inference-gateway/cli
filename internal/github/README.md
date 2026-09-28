# github

**What** - the GitHub capability: resolving `#` issue references typed in chat, and installing the
OpenTask GitHub workflow.
**Why** - GitHub context and workflow setup are tasks the agent must do, but there is no built-in
GitHub tool: the work goes through the `gh` CLI and the API.
**How** - the agent calls a service port, and `issues/` and `setup/` implement it by shelling out to `gh` and fetching workflow files.

## What it owns

- `issues/service.go` - resolving the current repository's open issues and expanding a `#N` token into
the issue's title, body and recent comments.
- `setup/install.go`, `setup/service.go` - installing or updating `.github/workflows/tasks.yml` for
  `infer-action` on an install branch, then opening a pull request.
- `setup/install_prompt.go` - the prompt used by the `/install-opentask` shortcut.

## Why it is separate

Both features are optional and environment-dependent (`gh` may be missing, the repo may have no
remote). Isolating them lets the agent degrade gracefully: a `#` reference is a no-op when `gh` is
unavailable, rather than a hard failure.

## How it plugs in

- Exposed through the GitHub service port in `internal/agent/domain`, so the agent loop never imports this package.
- `gh` must be installed and authenticated; `GITHUB_TOKEN` is used for the API where relevant.
- There is deliberately no built-in GitHub tool. Bash with the `gh` CLI, the `/scm` shortcuts, and `/install-opentask` are the supported paths.

## Related

- [Shortcuts Guide](../../docs/shortcuts-guide.md)
- [Commands Reference](../../docs/commands-reference.md)
- [Subagents](../../docs/subagents.md)
