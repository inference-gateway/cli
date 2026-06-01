# GitHub Issues Reference

Use this reference when asked to file or draft issues in `inference-gateway/*`, or when CI/sync workflows need stable issue
metadata.

## Template Sources

Always check target-repo templates first:

```sh
gh api repos/inference-gateway/<repo>/contents/.github/ISSUE_TEMPLATE
```

If the target repo has no override, use org defaults from `inference-gateway/.github`:

```sh
gh api repos/inference-gateway/.github/contents/.github/ISSUE_TEMPLATE
gh api repos/inference-gateway/.github/contents/.github/ISSUE_TEMPLATE/bug_report.md -H 'Accept: application/vnd.github.raw'
```

Canonical org-default templates:

| Template                   | Title prefix      | Labels          | Type            |
| -------------------------- | ----------------- | --------------- | --------------- |
| `bug_report.md`            | `[BUG]`           | `bug`           | `bug`           |
| `feature_request.md`       | `[FEATURE]`       | `enhancement`   | `feature`       |
| `documentation_request.md` | `[DOCS]`          | `documentation` | `documentation` |
| `refactor_request.md`      | `[TASK] Refactor` | `refactor`      | `task`          |

## Filing Procedure

1. Read the relevant template file to get the exact current sections. Do not paraphrase from memory.
2. Fill every section the template defines. Use HTML comments as prompts, but remove them from the final body.
3. Match the stable title prefix exactly. Sync orchestrators rely on byte-exact matching.
4. Set labels with `gh issue create --label`; template frontmatter applies only in the GitHub UI.
5. Set Issue Type with GraphQL `updateIssueIssueType`. `gh issue create/edit` has no `--type` flag.
6. If the org lacks the requested issue type, warn and continue.
7. Do not include `@claude` in titles, bodies, or comments.
8. For `inference-gateway/docs`, keep titles, bodies, comments, and footers ASCII-only. Use `-`, not en dash or em dash.

## Docs Tickets

For `feat:` or public-surface `refactor:` changes outside `inference-gateway/docs`, prepare a `[DOCS]` issue against
`inference-gateway/docs` using `documentation_request.md`.

The draft must include:

- User-facing feature or changed behavior.
- Affected pages or proposed page locations.
- Code samples or examples to add, when applicable.
- Link back to the originating PR or issue.

Purely internal refactors may skip this only when the source PR body states `no docs ticket: internal-only refactor`.

## Drift Issue Titles

Use these exact titles for drift issues; do not improvise:

- `[FEATURE] Implement missing OpenAPI operations`
- `[TASK] Refactor regenerate models from latest spec`
- `[FEATURE] Add usage examples for missing operations`
- `[TASK] Refactor sync vendored openapi.yaml with schemas`
- `[DOCS] Document missing operations and schemas`
- `[FEATURE] Implement missing A2A JSON-RPC methods`
- `[TASK] Refactor regenerate A2A types from latest schema`
- `[DOCS] Add usage examples for missing A2A methods`
- `[TASK] Refactor sync vendored schema.yaml with schemas`
- `[TASK] Add tests for missing operations / methods`

Required labels:

- `sdk-drift` on `kind: sdk|docs` repos.
- `adk-drift` on `kind: adk` repos.

These labels must already exist on the target; orchestrators do not create them.
