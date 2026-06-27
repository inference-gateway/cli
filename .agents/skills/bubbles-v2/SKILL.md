---
name: bubbles-v2
description: >
  Use the Bubbles v2 component library for Bubble Tea v2 (charm.land/bubbles/v2):
  viewport, textinput/textarea, spinner, progress, list, table, key, help. Use when
  adding or reviewing prebuilt TUI components - scrollable viewports, text inputs,
  spinners, progress bars, key bindings and help - with this stack. Each component is
  a sub-model you embed and delegate to; v2 constructors and key handling differ from
  v1, so lead with the contract and the v1->v2 table.
license: Apache-2.0
---

# Bubbles v2

Prebuilt components for Bubble Tea v2. Import `charm.land/bubbles/v2/<component>`;
pinned to v2.1.0. Each component is itself a small Bubble Tea model that you embed in
your own model and delegate to.

## The component contract

Every bubble has `New(...)`, an `Update(tea.Msg) (Model, tea.Cmd)` that returns its
**concrete** type, and `View() string`. Embed it, delegate in `Update` (reassign the
field and collect the cmd), and place its view:

```go
import (
    tea "charm.land/bubbletea/v2"
    "charm.land/bubbles/v2/spinner"
    "charm.land/bubbles/v2/viewport"
    "charm.land/lipgloss/v2"
)

type model struct {
    vp viewport.Model
    sp spinner.Model
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmds []tea.Cmd
    var cmd tea.Cmd

    m.vp, cmd = m.vp.Update(msg) // reassign - dropping it loses state
    cmds = append(cmds, cmd)
    m.sp, cmd = m.sp.Update(msg)
    cmds = append(cmds, cmd)

    return m, tea.Batch(cmds...)
}

func (m model) View() tea.View {
    // components return string; compose, then wrap ONCE at the root
    body := lipgloss.JoinVertical(lipgloss.Left, m.sp.View(), m.vp.View())
    return tea.NewView(body)
}
```

That last step is the integration key: components produce **strings**; only the root
model wraps the composed string in `tea.NewView` (see **bubbletea-v2**).

## v2 vs the v1 you remember

| Topic | v1 | v2 |
| --- | --- | --- |
| Import | `github.com/charmbracelet/bubbles/...` | `charm.land/bubbles/v2/...` |
| Constructors | positional, e.g. `viewport.New(w, h)` | functional options: `viewport.New(viewport.WithWidth(w), ...)` |
| Default keymap | `DefaultKeyMap` variable | `DefaultKeyMap()` function |
| Sizing | public `Width`/`Height` fields | `SetWidth`/`SetHeight` methods |
| Keys | `tea.KeyMsg` | `tea.KeyPressMsg`; `key.Matches` is generic over `fmt.Stringer` |
| Styling | Lip Gloss v1 | Lip Gloss v2 (`Color()` func, `LightDark`) |

## Components you'll reach for

| Component | Purpose | Construct / must-know |
| --- | --- | --- |
| `viewport` | scrollable region | `New(WithWidth(w), WithHeight(h))`; `SetContent`, `SetWidth/SetHeight`, `DefaultKeyMap()` |
| `textinput` | one-line input | `New()`; `Focus() tea.Cmd`, `Blur()`, `Value()`, `SetValue()` |
| `textarea` | multi-line input | `New()`; `Focus()`, `Value()`, `SetWidth/SetHeight` |
| `spinner` | activity indicator | `New(WithSpinner(spinner.Dot))`; start with its `Tick` |
| `progress` | progress bar | `New(WithDefaultBlend())`; static `ViewAs` vs animated `SetPercent` |
| `list` | filterable list | `New(items, delegate, width, height)` (positional) |
| `table` | data grid | `New(WithColumns(...), WithRows(...))` |
| `key` | key bindings | `NewBinding(WithKeys("q"), WithHelp("q","quit"))`; `key.Matches(msg, b)` |
| `help` | keymap help line | `New()`; renders short/full help from your keymap |

## Notes on the common ones

**viewport** - size on resize, then set content:

```go
case tea.WindowSizeMsg:
    m.vp.SetWidth(msg.Width)
    m.vp.SetHeight(msg.Height - headerH)
// elsewhere:
m.vp.SetContent(body)
```

**spinner** - animation is a self-perpetuating command; kick it off in `Init` and let
its `TickMsg` flow back through the component's `Update`:

```go
func (m model) Init() tea.Cmd { return m.sp.Tick }
```

**progress** - two modes. Static: `m.pr.ViewAs(0.4)` renders a bar at a percentage you
own. Animated: `cmd := m.pr.SetPercent(0.4)`, then route `progress.FrameMsg` through
`m.pr.Update`.

**key + help** - define a keymap of `key.Binding`s, match with
`key.Matches(msg, m.keys.Up)`, and let `help` render it (implement `ShortHelp` /
`FullHelp` on your keymap).

## Best practices & gotchas

- Always reassign the component (`m.vp, cmd = m.vp.Update(msg)`) and `tea.Batch` the
  cmds - dropping either loses state or stops animation.
- Set component sizes from `tea.WindowSizeMsg`; account for borders/margins you wrap
  around them.
- Manage focus: exactly one input `Focus()`ed at a time; `Blur()` the rest. `Focus()`
  returns a `tea.Cmd` - don't drop it.
- Constructors take options now: `viewport.New(w, h)` won't compile; use `WithWidth`/
  `WithHeight`. (`list.New` is the exception - still positional.)
- `DefaultKeyMap` is a call now: `viewport.DefaultKeyMap()`.

Pairs with **bubbletea-v2** (the runtime) and **lipgloss-v2** (styling components).
Pinned to bubbles v2.1.0.
