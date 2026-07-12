# Skills

Skills are reusable, model-readable instruction folders that the agent loads
on demand. The Inference Gateway CLI uses the **same on-disk format** as per standard, so a folder
authored for any of those tools drops into `.infer/skills/` (or the
`.agents/skills/` open standard) unchanged.

## Format

A skill is a **directory** containing a `SKILL.md` file with YAML frontmatter
at the top:

```markdown
---
name: pdf-helper
description: Extract text from PDFs. Use when the user asks to read, summarise, or analyse a PDF file.
---

# PDF Helper

Step-by-step instructions for the model:

1. Use the Bash tool to invoke `pdftotext input.pdf -` and capture stdout.
2. If the PDF is image-only, fall back to `tesseract` for OCR.
3. ...
```

The directory may also ship optional helpers - `references/`, `scripts/`,
`assets/` - that the model reads (or executes via the `Bash` tool) once it
has activated the skill.

### Frontmatter

- `name` (required): ≤64 chars; lowercase letters, digits and hyphens only;
  must equal the directory name; must not contain `infer`, `claude`,
  `anthropic`, `gemini` or `openai`.
- `description` (required): non-empty, ≤1024 chars.

Unknown frontmatter keys are tolerated, so vendor extensions (e.g. Gemini's
`disabled:` flag, Anthropic's `allowed-tools:`) won't cause validation
failures even though the CLI ignores them.

## Locations

The CLI scans three directories, in precedence order (first match wins on a
`name` collision):

1. Project-local: `.infer/skills/<name>/SKILL.md`
2. Open standard: `.agents/skills/<name>/SKILL.md`
3. User-global: `~/.infer/skills/<name>/SKILL.md`

`.agents/skills/` is the emerging cross-tool convention (Claude Code, Gemini
CLI, Codex CLI), so a repo that ships skills there works without moving them
into `.infer/skills/`. A project's `.infer/skills/` still wins over both the
open-standard and user-global locations - useful for overriding a personal or
shared default with a per-project variant.

## Built-in skills

The CLI ships a small set of **built-in skills** embedded in the binary. On
`infer init` they are seeded into the user-global `~/.infer/skills/` - the same
directory the loader reads - **only if absent**, so they behave exactly like a
skill you authored there yourself. Current built-ins:

- **`tmux`** - drive interactive terminal programs (TUIs, REPLs, another CLI's
  chat UI) by scripting tmux with `send-keys` / `capture-pane`.

Because they are ordinary user-scope skills, you customise them with the same
knobs as any other skill - there is no special "built-in" mode:

- **Edit in place**: change `~/.infer/skills/tmux/SKILL.md`; a re-run of `infer
  init` never re-seeds over your edit.
- **Override per project**: a `.infer/skills/tmux/` (or `.agents/skills/tmux/`)
  shadows the built-in for that repo (first match wins).
- **Disable**: add the name to `agent.skills.disabled_skills`.
- **Reset to the shipped default**: `infer init --overwrite` re-seeds it. Note
  this refreshes the other shipped `~/.infer` defaults too, so use it when you
  want a clean baseline; to restore just one skill, replace its `SKILL.md` by
  hand (a plain `infer init` will not re-seed over an already-initialized home).

## Enabling

Skills are **disabled by default** (zero token cost when off). Enable via
config or environment variable:

```yaml
# .infer/config.yaml
agent:
  skills:
    enabled: true
    disabled_skills: []   # optional list of skill names to skip
```

```bash
INFER_AGENT_SKILLS_ENABLED=true infer chat
```

When enabled, the agent's system prompt gains an `AVAILABLE SKILLS:` block
listing each skill's `name`, `description`, scope, and the **absolute path**
to its `SKILL.md`. The body of `SKILL.md` is **not** loaded at startup - the
model reads it on demand using the existing `Read` tool. This is "progressive
disclosure" and matches the behaviour of other vendors.

## Discovering skills

```bash
infer skills list
```

This always works regardless of `agent.skills.enabled`, so you can verify
discovery before turning the feature on. The output shows each skill's name,
scope, description, absolute path, and any validation errors for skills that
were skipped.

## Installing skills from GitHub

You can install a skill folder directly from a GitHub repository:

```bash
infer skills install https://github.com/anthropics/skills/tree/main/skills/pdf
```

The URL must point at a **directory** inside the repo, formatted as
`https://github.com/<owner>/<repo>/tree/<ref>/<path-to-skill-folder>`.
URLs that point at a file (`/blob/`) or at the repo root are rejected
with a clear error.

Flags:

- `--user` - install to `~/.infer/skills/` instead of the project-local
  `.infer/skills/`.
- `--overwrite` - replace an existing skill folder of the same name.
  Without this flag, an existing folder is left untouched and the install
  fails fast.

After download, the same frontmatter validator that runs at startup runs
against the downloaded folder - so what installs is what loads. If
validation fails (missing `name`, name doesn't match the directory name,
etc.) the folder is removed and the reason is printed. There is never a
half-installed state.

### Authentication

Requests are unauthenticated by default, which GitHub limits to 60 API
requests per hour per IP - easily exhausted on shared CI runners. Set
`GITHUB_TOKEN` (or `GH_TOKEN`, matching the `gh` CLI) in the environment to
authenticate:

```bash
GITHUB_TOKEN="$MY_TOKEN" infer skills install acme/internal-comms
```

Authenticating raises the limit to 5,000 requests per hour and lets you
install from private repositories the token can access.

**Limitations:**

- Each install is one API call (the tree enumeration) plus one raw
  download per file in the skill folder. Without a token you share the
  60 requests/hour anonymous limit with everything else on your IP.
- Refs containing a literal `/` (e.g. `feature/foo` branches) are not
  supported. Use a tag, the default branch, or a single-segment branch.

## Uninstalling skills

```bash
infer skills uninstall pdf
infer skills uninstall --user internal-comms
```

The argument is the on-disk skill directory name (matching the skill's
`name` frontmatter). By default the project-local `.infer/skills/<name>/`
is removed; pass `--user` to remove from `~/.infer/skills/<name>/`.

There is no confirmation prompt - matches `npm uninstall`,
`brew uninstall`, etc. The skill name is regex-validated before any
filesystem operation, so it cannot be used to traverse outside the
configured skills directory.

## Authoring tips

- **Make `description` actionable.** It is the routing signal. Tell the
  model both *what* the skill does and *when* it should activate. "Extract
  text from PDFs. Use when the user asks to read, summarise, or analyse a
  PDF file." is a good description.
- **Keep `SKILL.md` focused.** Use `references/` for long supporting docs;
  link to them from `SKILL.md` so the model only reads them when needed.
- **Use the existing tools.** The model already has `Read`, `Bash`, etc.
  Skills are instructions on top of those, not new capabilities.

## Security

Skills can instruct the model to run shell commands, read files, or call
external APIs. Treat a skill like any other piece of executable content -
**only install skills from trusted sources**. The CLI's normal tool-approval
system still gates each command, but a malicious skill could craft a
plausible-looking `Bash` call.

The frontmatter `name` validator rejects names containing vendor strings
(`claude`, `anthropic`, `gemini`, `openai`, `infer`) so impersonating an
official skill is harder.

## Portability

The on-disk contract is intentionally identical to:

- Anthropic Claude Code - <https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview>
- Google Gemini CLI - <https://github.com/google-gemini/gemini-cli/blob/main/docs/cli/skills.md>
- OpenAI Codex CLI - <https://simonwillison.net/2025/Dec/12/openai-skills/>

Folders from `github.com/anthropics/skills` and `github.com/google/skills`
work without modification when copied into `.infer/skills/` or `.agents/skills/`.

## Out of scope (for now)

- A dedicated `activate_skill` tool - the model uses `Read` directly.
- A skill marketplace or curated index - discovery is up to the user; see
  [Installing skills from GitHub](#installing-skills-from-github) for the
  install flow.
- Authenticated installs from private repositories.
- Sandboxing beyond what the existing tool-approval system already provides.
