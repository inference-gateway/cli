---
name: lipgloss-v2
description: >
  Style and lay out terminal output in Go with Lip Gloss v2 (charm.land/lipgloss/v2)
  - styles, colors, borders, layout joins, and tables. Use when writing or reviewing
  terminal styling: foreground/background colors, padding/border/width, JoinHorizontal/
  JoinVertical, Place, or lipgloss/v2/table, usually alongside Bubble Tea v2. v2's
  color and renderer model changed sharply from v1 - lead with the v1->v2 table so you
  don't reach for AdaptiveColor or a global renderer that no longer exist.
license: Apache-2.0
---

# Lip Gloss v2

Declarative terminal styling and layout. Import `charm.land/lipgloss/v2` (alias
`lipgloss`); pinned to v2.0.4. Styles are immutable values you build once and reuse.
The sharp break from v1 is the color/renderer model, so start there.

## v2 vs the v1 you remember

| Topic | v1 | v2 |
| --- | --- | --- |
| Import | `github.com/charmbracelet/lipgloss` | `charm.land/lipgloss/v2` |
| Color | `lipgloss.Color` is a string type | `lipgloss.Color(s)` is a **func** → `image/color.Color` |
| Renderer | global default renderer, `SetColorProfile` | none - styles are pure values; downsampling at print / Bubble Tea |
| Adaptive | `lipgloss.AdaptiveColor{Light,Dark}` | gone from root → `lipgloss.LightDark(isDark)(l,d)` or `compat.AdaptiveColor` |
| Dark-bg detect | `HasDarkBackground()` (no args) | `HasDarkBackground(in, out)` or Bubble Tea `BackgroundColorMsg` |
| Print | `fmt.Println(s.Render(...))` | `lipgloss.Println(...)` so color degrades to the terminal |

## Styles

Immutable - each setter returns a copy. Build once at package/model scope, reuse:

```go
var title = lipgloss.NewStyle().
    Bold(true).
    Foreground(lipgloss.Color("#7D56F4")).
    Background(lipgloss.Color("236")).
    Padding(0, 1).
    Border(lipgloss.RoundedBorder()).
    BorderForeground(lipgloss.Color("63")).
    Width(40)

out := title.Render("Hello")
```

Common setters: `Bold/Italic/Faint/Underline/Reverse`, `Foreground/Background`,
`Padding/Margin` (1–4 ints, CSS order), `Width/Height/MaxWidth`,
`Align/AlignHorizontal/AlignVertical`, `Border/BorderForeground`, `Inline`. Compose
shared rules with `.Inherit(base)`.

## Color

`lipgloss.Color(s)` accepts ANSI (`"9"`), 256 (`"201"`), or truecolor (`"#04B575"`)
and returns a standard `color.Color`:

```go
fg := lipgloss.Color("#04B575")
```

Adaptive light/dark - `AdaptiveColor` is gone from the root package. Choose by
background instead:

```go
dark := lipgloss.HasDarkBackground(os.Stdin, os.Stdout) // standalone programs
ld := lipgloss.LightDark(dark)
fg := ld(lipgloss.Color("#333"), lipgloss.Color("#eee")) // ld(light, dark)
```

Inside Bubble Tea v2 don't query stdio yourself: return `tea.RequestBackgroundColor`
from `Init`, handle `tea.BackgroundColorMsg` (`msg.IsDark()`), and rebuild styles via
`LightDark`. For a quick v1 port, `compat.AdaptiveColor{Light, Dark}` still works
(reads stdio globally, like v1). Color-profile degradation is automatic when you
print via `lipgloss.Println`/`Print`/`Sprint` (standalone) or render inside Bubble
Tea; for explicit control use `lipgloss.Complete(profile)`.

## Layout

```go
row := lipgloss.JoinHorizontal(lipgloss.Top, left, right)    // align on cross axis
col := lipgloss.JoinVertical(lipgloss.Center, head, body)
banner := lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box)
```

Measure rendered strings with `lipgloss.Width` / `Height` / `Size` - they ignore ANSI
escapes and count wide runes correctly. **Never size styled text with `len(s)`.**

## Borders

`NormalBorder()`, `RoundedBorder()`, `ThickBorder()`, `DoubleBorder()`,
`HiddenBorder()` (for spacing), or a custom `lipgloss.Border{...}`. Control sides and
color with `BorderTop(true)`, `BorderForeground(c)`, `BorderTopForeground(c)`.

## Tables (`charm.land/lipgloss/v2/table`)

```go
import "charm.land/lipgloss/v2/table"

t := table.New().
    Headers("NAME", "LANG").
    Row("Bubble Tea", "Go").
    Row("Lip Gloss", "Go").
    Border(lipgloss.RoundedBorder()).
    StyleFunc(func(row, col int) lipgloss.Style {
        if row == table.HeaderRow { // HeaderRow == -1
            return header
        }
        return cell
    })

fmt.Println(t) // *table.Table is a Stringer
```

## Best practices & gotchas

- Define styles once; building a `NewStyle()` chain inside `View()` every frame is
  wasteful.
- Size with `lipgloss.Width`, not `len` - ANSI codes and wide glyphs make byte length
  wrong.
- Standalone output: print with `lipgloss.Println` so truecolor degrades on limited
  terminals. Inside Bubble Tea, just return the styled string from your View.
- Don't look for `AdaptiveColor`, `SetColorProfile`, or a `Renderer` - use
  `LightDark`, explicit `Complete`, or the `compat` package.

Pairs with **bubbletea-v2** (the View you style) and **bubbles-v2** (components you
theme). Pinned to lipgloss v2.0.4.
