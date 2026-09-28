# AGENTS.md

Agent rules for `internal/presentation`. They take precedence over the root `AGENTS.md` for files in this directory.

- Write Bubble Tea v2 (`charm.land/bubbletea/v2`, alias `tea`). The v2 API differs sharply from v1: constructors, key handling and message types all changed. Read `.agents/skills/bubbletea-v2/SKILL.md` before editing a `tea.Model`.
- Build UI from Bubbles v2 components (`charm.land/bubbles/v2`): viewport, textinput, textarea, spinner, progress, list, table, tree, key, help. Read `.agents/skills/bubbles-v2/SKILL.md` before adding one.
- Subpackages: `tui/` (chat TUI), `headless/`, `telegram/`, `web/`, `shortcuts/`. Keep each sub-model's state, `Update` and `View` in its own file; the root model only routes messages.
- Styling, themes and layout belong here and only here. Keep palette and theme values in the TUI theme, not in feature code.
- `telegram/` is the only place go-telegram appears.
