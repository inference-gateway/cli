# File Explorer: Snippet Selection and Annotation

The `/explorer` command opens a VS Code-style file browser with a syntax-highlighted
preview pane. In addition to browsing files, you can **select a line range** within a
previewed file, **annotate it** with a natural-language instruction, and **inject** the
annotated snippet plus surrounding file context into the chat - so the LLM knows exactly
which code to change and how.

## Workflow

1. Open the explorer with `/explorer`.
2. Navigate the tree (`j`/`k` or arrow keys) to a file and let the preview render.
3. Press `s` to enter **select mode** on the previewed file.
4. Move the line cursor with `j`/`k`. Press `space` (or `v`) to anchor the other end of
   the range. The selected lines are highlighted with a `▌` gutter; the cursor line shows
   a `▶` gutter.
5. Press `a` to open the **annotation input**. Type your instruction (e.g. "refactor this
   function to use early returns") and press `enter` to confirm.
6. Repeat steps 4–5 for additional disjoint ranges (in the same file or navigate to
   another file after exiting select mode with `esc`).
7. Press `enter` (submit) to inject all annotated snippets into the chat input. Review
   the formatted context and press `enter` again to send.

## Keybindings

All keys are configurable via `keybindings.yaml` under the `explorer` namespace.

| Action          | Default      | Description                                      |
|-----------------|--------------|--------------------------------------------------|
| `select`        | `s`          | Enter line-selection mode on the previewed file  |
| `toggle_select` | `space`, `v` | Start or clear a line-range selection            |
| `annotate`      | `a`          | Annotate the selected range with an instruction  |
| `submit`        | `enter`      | Submit annotations and inject into chat          |
| `cancel`        | `esc`, `q`   | Exit select mode (`esc`) or close explorer (`q`) |

## Injected Context Format

On submit, the explorer builds a structured prompt grouped by file:

- For files under 1 MiB, the **full file** is included in a fenced code block with line
  numbers, followed by per-selection instruction lines.
- For larger files, each snippet is included individually with a ±5-line context window
  and a `▶` marker on the annotated lines.

The prompt is placed in the chat input for review before sending, so you can edit or
add context before the LLM sees it.

## Multi-Snippet and Cross-File Support

You can annotate multiple disjoint ranges in a single file, and ranges across different
files. Each annotation is stored independently as a `(file, start_line, end_line,
annotation)` tuple. Navigate to another file by pressing `esc` to exit select mode, then
use tree navigation and re-enter select mode with `s`.
