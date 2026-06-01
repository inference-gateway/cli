# Ecosystem Reference

Use this reference for repo ownership, CI behavior, cross-repo reads, and org-wide conventions.

## Repo Map

The `inference-gateway` GitHub organization. Each repository is separately versioned and released. Refer to
repos by their `inference-gateway/<repo>` GitHub slug rather than local paths.

| Repo (`inference-gateway/...`) | Purpose                                                                            |
| ------------------------------ | ---------------------------------------------------------------------------------- |
| `inference-gateway`            | Main Go HTTP gateway. Source of truth for provider configs via `openapi.yaml`.     |
| `cli`                          | Command-line client (`infer`).                                                     |
| `operator`                     | Kubernetes operator.                                                               |
| `sdks`                         | Multi-language SDKs generated from `schemas`.                                      |
| `schemas`                      | Shared OpenAPI/JSON schemas consumed by SDKs and docs.                             |
| `docs`                         | Next.js + MDX documentation site.                                                  |
| `adl`, `adl-cli`               | Agent Definition Language and its CLI.                                             |
| `a2a-debugger`                 | Agent-to-agent tooling.                                                            |
| `*-agent`                      | Agent-to-agent agents.                                                             |
| `awesome-a2a`                  | Curated awesome-list for A2A.                                                      |
| `registry`                     | Component / agent registry.                                                        |
| `infer-action`                 | GitHub Action wrapper.                                                             |
| `tools`                        | Shared tooling.                                                                    |
| `skills`                       | Public Agent Skills catalog, distinct from private/local skills directories.       |
| `.github`                      | Org-level community health repo with default issue templates and shared workflows. |

Every repo may have its own `CLAUDE.md` or `AGENTS.md`; read local instructions before assuming conventions.

## Determining Context

- In CI, use `$GITHUB_REPOSITORY` (`owner/repo`) and `$GITHUB_WORKSPACE`.
- Locally, use `gh repo view --json nameWithOwner -q .nameWithOwner` or `git remote get-url origin`.
- Treat the current working directory as the current repo. Do not assume sibling repos are checked out.

## Reading Other Repos

Use `gh` when another org repo is not present locally:

```sh
gh api repos/inference-gateway/<repo>/contents/<path> -H 'Accept: application/vnd.github.raw'
gh api repos/inference-gateway/<repo>/contents/<dir>
gh api repos/inference-gateway/<repo>/contents/<path>?ref=<branch-or-sha> -H 'Accept: application/vnd.github.raw'
gh repo clone inference-gateway/<repo> -- --depth=1
```

These work locally and in GitHub Actions when `GH_TOKEN` or `GITHUB_TOKEN` has access to the target repo.

## Cross-Repo Checklist

- Provider added/changed in `openapi.yaml` -> docs page in `inference-gateway/docs` (`markdown/`)?
- New config env var -> regenerated `Configurations.md`, Helm `values.yaml`, and `examples/docker-compose/*/.env.example`?
- Schema change in `schemas` -> SDK regeneration in `sdks`?
- Main gateway release -> `operator` and Helm chart point at the new image tag?

When running in CI without an interactive user, put this scope note in the PR body, job summary, or issue comment.

## House Conventions

- Commit style: Conventional Commits. Semantic-release reads commit types to compute versions.
- Releases: semantic-release plus goreleaser where applicable. Do not hand-edit `CHANGELOG.md`.
- Go style: early returns, switch over if/else chains, lowercase log messages, code to interfaces, table-driven tests with `t.Run`.
- Mocks: generated via `mockgen` into `tests/mocks/` in the main gateway; refresh with `go generate ./...` or `task generate`.
- Main gateway pre-push confidence path: `task generate && task format && task lint && task build && task test`.
- Vision/multimodal: disabled by default (`ENABLE_VISION=false`); enable explicitly when testing vision flows.

## CI / GitHub Actions

- Prefer `$GITHUB_REPOSITORY`, `$GITHUB_REF`, `$GITHUB_SHA`, `$GITHUB_WORKSPACE`, and `$GITHUB_EVENT_PATH` over inference.
- Authenticate `gh` with `GH_TOKEN=${{ secrets.GITHUB_TOKEN }}` or a PAT for cross-repo access.
- Default `GITHUB_TOKEN` is usually scoped to the current repo. Cross-repo writes require a token with target access.
- Use paths relative to `$GITHUB_WORKSPACE` or `.`.
- Put human-facing results in the PR/issue body, job summary (`$GITHUB_STEP_SUMMARY`), or comment.
- Treat CI as ephemeral: no sibling repos, no developer-machine tools, no writes outside the workspace.

## Do Not

- Do not run `task generate`, push, force-push, or cut releases without explicit confirmation or an explicitly authorized CI step.
- Do not hand-edit generated files.
- Do not assume one repo's `CLAUDE.md` applies to another.
- Do not conflate this public `inference-gateway/skills` catalog with a private Claude Code or Codex skills directory.
- Do not bypass hooks (`--no-verify`, `--no-gpg-sign`) unless the user explicitly asks.
- When unsure which repo owns a concern, search the org: `gh search code --owner inference-gateway '<term>'`.
