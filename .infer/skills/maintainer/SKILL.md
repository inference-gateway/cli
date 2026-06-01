---
name: maintainer
description: >
  Maintain the `inference-gateway` GitHub org. Use inside any org repo, in local or CI checkouts, for cross-repo
  changes such as OpenAPI/schema updates, releases, docs follow-up, GitHub issues, or ecosystem-level questions.
license: Apache-2.0
---

# Inference Gateway Maintainer

Use this skill for ecosystem maintenance across `inference-gateway/*` repos. Keep the current repo's local instructions
authoritative, and load references only when the task needs them.

## Start here

1. Read the current repo's `CLAUDE.md` or `AGENTS.md` before changing files.
2. Identify the current repo from `$GITHUB_REPOSITORY`, `gh repo view --json nameWithOwner -q .nameWithOwner`, or
   `git remote get-url origin`.
3. Treat the working directory as the only checked-out repo. Use `gh` to inspect other org repos unless the user confirms
   sibling clones exist.
4. Use the repo's `Taskfile.yml` when present. Run `task --list` before guessing commands.
5. Do not hand-edit generated files. Change the generator input (`openapi.yaml`, schemas, templates) and regenerate only
   with explicit confirmation when generation has broad impact.

## Load references

- Read [references/ecosystem.md](references/ecosystem.md) when you need the repo map, org-wide conventions, CI/GitHub
  Actions behavior, cross-repo read commands, or the full "do not do" list.
- Read [references/github-issues.md](references/github-issues.md) when asked to file or draft issues, choose issue
  templates, set labels/types, or handle drift/sync issue titles.

## Cross-repo impact

Before shipping, surface affected repos explicitly when a change can ripple:

- Main gateway `openapi.yaml` changes may affect `sdks`, `docs`, `cli`, and `operator`.
- `schemas` changes may affect `sdks` and `docs`.
- Provider additions usually need docs under `inference-gateway/docs` and may need examples.
- Gateway releases may require `operator`, Helm, image tag, and example manifest updates.
- New config env vars may require regenerated configuration docs, Helm values, and example `.env` files.

Do not fan out edits silently. List impacted repos in the response, PR body, issue comment, or job summary.

## Docs follow-up rule

Every `feat:` or public-surface `refactor:` outside `inference-gateway/docs` needs a matching `[DOCS]` issue for
`inference-gateway/docs`, created with the source PR rather than later.

- Default to preparing a docs ticket draft: title, body, affected pages, and source PR/issue link.
- Purely internal refactors may skip the docs issue only if the PR body states `no docs ticket: internal-only refactor`.
- Do not file cross-repo issues automatically unless the user explicitly asks.
- For exact issue template handling, read [references/github-issues.md](references/github-issues.md).

## Maintainer defaults

- Use Conventional Commits: `feat:`, `fix:`, `docs:`, `chore:`, `test:`, `refactor:`, optionally scoped.
- Do not hand-edit generated `CHANGELOG.md`, release metadata, or generated code.
- Do not run push, force-push, cut releases, or bypass hooks without explicit confirmation.
