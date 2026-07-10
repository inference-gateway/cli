---
name: huh-v2
description: >
  Build interactive terminal forms and prompts with huh v2 (charm.land/huh/v2):
  Form/Group/Field composition, Select/MultiSelect/Input/Confirm/Text/FilePicker,
  embedding forms inside a Bubble Tea v2 model, standalone Run() prompts, theming,
  and filtering. Use when adding or reviewing any interactive form, picker, wizard,
  confirm, or approval prompt with this stack. v2's Theme became an interface and
  forms complete via internal command messages, so lead with the contract and the
  v1->v2 table - never write from v1 memory.
license: Apache-2.0
---

# Huh v2

Terminal forms for Bubble Tea v2. Import `charm.land/huh/v2`; pinned to v2.0.3.
A `Form` is a sequence of `Group`s (one visible at a time); a `Group` holds
`Field`s. Values bind to your variables via `.Value(&x)`.

## The contract: embedding a form in a Bubble Tea model

The canonical in-repo example is
`internal/ui/components/init_github_action_view.go`. Hold a `*huh.Form`,
delegate `Update`, type-assert the returned model, switch on `form.State`,
and rebuild + `Init()` a fresh form per phase:

```go
type wizard struct {
    form *huh.Form
    name string
}

func (w *wizard) buildForm() *huh.Form {
    return huh.NewForm(
        huh.NewGroup(
            huh.NewInput().Title("Name").Validate(notEmpty).Value(&w.name),
        ),
    ).WithWidth(w.width).WithTheme(huhTheme(w.styleProvider))
}

func (w *wizard) Init() tea.Cmd { return w.form.Init() }

func (w *wizard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    form, cmd := w.form.Update(msg)
    if f, ok := form.(*huh.Form); ok {
        w.form = f
    }
    switch w.form.State {
    case huh.StateCompleted: // bound values are now set
        return w, w.advance() // next phase: rebuild form, return form.Init()
    case huh.StateAborted:   // quit key pressed (ctrl+c by default)
        return w, tea.Quit
    }
    return w, cmd
}

func (w *wizard) View() tea.View { return tea.NewView(w.form.View()) }
```

**Every message must reach the form.** Enter/next/submit work through huh's
internal command messages (`nextFieldMsg`, `nextGroupMsg`): the field returns
a cmd, the runtime executes it, and the resulting message must be routed back
into `form.Update` or the form stalls one step before completion. This is
automatic when the form's owner is the top-level model; when the form floats
inside a larger app (see `question_form_view.go` + `forwardToOverlayForms` in
`internal/app/chat.go`), forward non-key messages explicitly. The same applies
to tests: pump returned cmds back through the form instead of asserting after
a single `Update` call.

## v2 vs the v1 you remember

| Topic | v1 | v2 |
| --- | --- | --- |
| Import | `github.com/charmbracelet/huh` | `charm.land/huh/v2` |
| `Theme` | struct | **interface** `Theme(isDark bool) *Styles`; adapt funcs with `huh.ThemeFunc` |
| Built-in themes | `huh.ThemeCharm()` returns `*Theme` | `huh.ThemeCharm(isDark bool) *Styles` (same for Dracula/Catppuccin/Base/Base16) |
| Accessible mode | per-field `WithAccessible` | form-level only: `Form.WithAccessible(true)`; field methods removed |
| Bubble Tea types | v1 `tea.Cmd`/`tea.Msg` | `charm.land/bubbletea/v2` throughout |
| Styling types | Lip Gloss v1 | Lip Gloss v2 (see **lipgloss-v2**) |
| Button alignment | v1 `lipgloss.Position` | v2 `lipgloss.Position` |
| Form model | unexported | `huh.Model` exported; `Form.View()` returns `string` |

## Fields you'll reach for

| Field | Purpose | Must-know |
| --- | --- | --- |
| `Input` | one-line text | `.Validate(func(string) error)`, `.Placeholder` |
| `Text` | multi-line text | `.Lines(n)` |
| `Select[T]` | pick one | generic over the value type; `.Height(n)` viewport; `/` filter built in; `.Inline(true)` = one-line ←/→ carousel; `.OptionsFunc` for dynamic options; `.GetFiltering()` |
| `MultiSelect[T]` | pick many | toggle with space/x; `.Validate` for "at least one" |
| `Confirm` | yes/no | `.Affirmative("Yes")` / `.Negative("No")` |
| `FilePicker` | pick a file | `.CurrentDirectory`, `.AllowedTypes([]string{".pem"})`, `.Height` |
| `Note` | static text | display-only card |

Options carry a display key and a typed value: `huh.NewOption("label", value)`.
`huh.NewOptions("a", "b")` when key == value.

## Best practices & gotchas

- **Forms are one-shot.** After `StateCompleted`/`StateAborted` a form is
  done; build a new one (and run its `Init()`) to show it again. Reset bound
  variables when rebuilding - they keep their last values.
- **Quit is ctrl+c only by default; esc does NOT abort.** Rebind when needed:
  `km := huh.NewDefaultKeyMap(); km.Quit = key.NewBinding(key.WithKeys("esc"))`
  then `.WithKeyMap(km)`. Beware: form-level Quit wins over field keys, so
  binding esc to Quit breaks Select's esc-clears-filter.
- **Enter is both next-field and submit** in the default keymap; the last
  group's completion submits the form.
- **Select's `/` filter is substring, not fuzzy**, and matches `option.Key` -
  including any suffix text you baked into the label.
- **Conditional groups**: `huh.NewGroup(...).WithHideFunc(func() bool)` -
  evaluated live, the standard trick for "show this input only when X chosen"
  (see the private-key fallbacks in `init_github_action_view.go`).
- **Theming**: this repo maps the active style-provider palette via
  `huhTheme(styleProvider)` in `internal/ui/components/huh_theme.go` - use it
  on every form (`.WithTheme(huhTheme(p))`). The theme is captured at build
  time; the rebuild-per-phase pattern picks up theme switches for free.
- **Plain-terminal prompts** (outside any Bubble Tea program): call `Run()`
  directly - `huh.NewConfirm().Title("Proceed?").Value(&ok).Run()` runs its
  own mini program (see `confirmInstall` in `cmd/plugins.go`). Guard for
  non-interactive stdin first.
- **Sizing**: overlay forms never see `tea.WindowSizeMsg`; set `.WithWidth`
  from your component's own width at build time.

Pairs with **bubbletea-v2** (the runtime), **bubbles-v2** (components), and
**lipgloss-v2** (styling). Pinned to huh v2.0.3.
