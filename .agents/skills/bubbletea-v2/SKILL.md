---
name: bubbletea-v2
description: >
  Build and review Bubble Tea v2 terminal UIs in Go (charm.land/bubbletea/v2) —
  The Elm Architecture: a tea.Model with Init/Update/View plus tea.Cmd/tea.Msg.
  Use whenever writing or reviewing a TUI, a tea.Model, Update/View methods,
  commands, key/mouse handling, or full-screen terminal apps with this stack,
  even if "Bubble Tea" isn't named. v2's API differs sharply from v1 — lead with
  the v1->v2 table so you don't write v1 code that won't compile.
license: Apache-2.0
---

# Bubble Tea v2

The Elm-Architecture TUI runtime. Import `charm.land/bubbletea/v2` (alias `tea`);
pinned to v2.0.7. State lives in a `Model`; messages drive `Update`, which returns
a new model and optional commands; `View` renders. The biggest risk here is v1
muscle memory, so start with what changed.

## v2 vs the v1 you remember

| Topic | v1 | v2 |
| --- | --- | --- |
| Import | `github.com/charmbracelet/bubbletea` | `charm.land/bubbletea/v2` |
| `View()` returns | `string` | **`tea.View`** (wrap via `tea.NewView(s)`) |
| Full-screen / mouse | `WithAltScreen()` / `WithMouseCellMotion()` opts | **View fields** `v.AltScreen=true`, `v.MouseMode` (opts gone) |
| Cursor / title / focus | imperative cmds | View fields: `v.Cursor`, `v.WindowTitle`, `v.ReportFocus` |
| Keys | `tea.KeyMsg` struct (`msg.Type`, `msg.Runes`) | **`tea.KeyPressMsg`** / `KeyReleaseMsg`; match `msg.String()` |
| Quit | `return m, tea.Quit` | same — `tea.Quit` is a `Cmd`, pass it, don't call it |

## The Model interface (exact v2)

```go
type Model interface {
    Init() tea.Cmd                       // initial command, or nil
    Update(tea.Msg) (tea.Model, tea.Cmd) // handle a message → new model + command
    View() tea.View                      // render — note: tea.View, NOT string
}
```

## Minimal program

```go
package main

import (
    "fmt"

    tea "charm.land/bubbletea/v2"
)

type model struct{ count int }

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyPressMsg:
        switch msg.String() {
        case "q", "ctrl+c":
            return m, tea.Quit // tea.Quit is a Cmd — pass it, never tea.Quit()
        case "+":
            m.count++
        }
    }
    return m, nil
}

func (m model) View() tea.View {
    return tea.NewView(fmt.Sprintf("count: %d  (+ add, q quit)\n", m.count))
}

func main() {
    if _, err := tea.NewProgram(model{}).Run(); err != nil {
        panic(err)
    }
}
```

## Control the terminal via the View (not program options)

In v2 the `View` struct carries terminal state. Build it, set fields, return it:

```go
func (m model) View() tea.View {
    v := tea.NewView(m.render())
    v.AltScreen = true                    // full-screen (alternate buffer)
    v.MouseMode = tea.MouseModeCellMotion // or MouseModeAllMotion / MouseModeNone
    v.WindowTitle = "my app"
    v.ReportFocus = true                  // deliver FocusMsg / BlurMsg
    v.Cursor = tea.NewCursor(m.cx, m.cy)  // *tea.Cursor; nil hides it
    return v
}
```

`WithAltScreen`, `WithMouseCellMotion`, `WithMouseAllMotion`, `WithReportFocus` no
longer exist. The real `tea.NewProgram` options are `WithContext`, `WithInput`,
`WithOutput`, `WithFPS`, `WithFilter`, `WithColorProfile`, `WithWindowSize`,
`WithEnvironment`, `WithoutSignalHandler`, `WithoutRenderer`, `WithoutCatchPanics`.

## Commands & messages

A `tea.Cmd` is `func() tea.Msg` — the only place to do I/O. Never mutate the model
from a bare goroutine; return a command and handle its message in `Update`.

```go
type gotData struct {
    body string
    err  error
}

func fetch(url string) tea.Cmd {
    return func() tea.Msg {
        resp, err := http.Get(url)
        if err != nil {
            return gotData{err: err}
        }
        defer resp.Body.Close()
        b, _ := io.ReadAll(resp.Body)
        return gotData{body: string(b)}
    }
}
```

- `tea.Batch(c1, c2)` runs commands concurrently; `tea.Sequence(c1, c2)` in order.
- `tea.Tick(d, fn)` / `tea.Every(d, fn)` are timers (`fn` returns a `tea.Msg`).
- `tea.Quit` exits; `tea.Printf` / `tea.Println` print above the UI.
- From outside the loop, push messages with `p.Send(msg)`.

## Input

- Keys: `case tea.KeyPressMsg:` then `switch msg.String()` (`"enter"`, `"ctrl+c"`,
  `"a"`, `"shift+tab"`, …). `KeyReleaseMsg` only arrives with keyboard enhancements.
- Resize: `case tea.WindowSizeMsg: m.w, m.h = msg.Width, msg.Height` — propagate to
  child components.
- Mouse: `tea.MouseClickMsg`, `MouseReleaseMsg`, `MouseMotionMsg`, `MouseWheelMsg`
  (all satisfy `tea.MouseMsg`); sent only when `v.MouseMode != MouseModeNone`.
- Focus/paste: `tea.FocusMsg` / `tea.BlurMsg` (needs `v.ReportFocus`), `tea.PasteMsg`.
- Light/dark: return `tea.RequestBackgroundColor` from `Init`, handle
  `tea.BackgroundColorMsg` (`msg.IsDark()`), rebuild styles — see **lipgloss-v2**.

## Best practices

- Keep `Update`/`View` pure and fast; offload I/O and sleeps to commands (a
  `time.Sleep` in `Update` freezes the UI — use `tea.Tick`).
- Compose: give each child its own model and have the parent delegate (route
  messages down, join views) — see **bubbles-v2** for the embed-and-delegate pattern.
- Value receivers are idiomatic; the runtime swaps in whatever model you return, so
  always return the updated copy.
- Cancel cleanly with `tea.NewProgram(m, tea.WithContext(ctx))`.

## Gotchas

- `View()` must return `tea.View` — wrap your string with `tea.NewView(...)`.
- Return `tea.Quit` (a `Cmd`); calling `tea.Quit()` yields a `Msg` and is wrong here.
- Never call another model's `Update` directly, and never touch the model from a
  goroutine — return a command instead.
- `tea.Batch` is unordered; use `tea.Sequence` when order matters.
- Reaching for `tea.WithAltScreen()`? Gone — set `v.AltScreen = true` in the View.

Pairs with **lipgloss-v2** (styling the strings in your View) and **bubbles-v2**
(prebuilt components). Pinned to bubbletea v2.0.7 — verify against the installed
module, not v1 memory.
