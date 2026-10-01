# File Explorer: Snippet Selection and Annotation

The `/explorer` command opens a VS Code-style file browser with a syntax-highlighted
preview pane. In addition to browsing files, you can **select a line range** within a
previewed file, **annotate it** with a natural-language instruction, and **attach** the
annotated snippet to your next message - so the LLM knows exactly
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
7. Press `enter` (submit) to attach all annotated snippets to your next message. They stay
   listed below the chat input, and their formatted context is sent with that message.

## Keybindings

All keys are configurable via `keybindings.yaml` under the `explorer` namespace.

| Action          | Default      | Description                                      |
|-----------------|--------------|--------------------------------------------------|
| `select`        | `s`          | Enter line-selection mode on the previewed file  |
| `toggle_select` | `space`, `v` | Start or clear a line-range selection            |
| `annotate`      | `a`          | Annotate the selected range with an instruction  |
| `submit`        | `enter`      | Attach annotations to the next message           |
| `cancel`        | `esc`, `q`   | Exit select mode (`esc`) or close explorer (`q`) |

## Attached Context Format

Only the annotated line ranges are sent, never the whole file and no surrounding context.
When the message carrying the selections is sent, the app appends one context block grouped
by file:

- Each selection becomes a fenced code block headed by `<file> (lines <start>-<end>):`,
  holding exactly the selected lines, with the file extension as the fence language.
- A `note: <annotation>` line follows the block when you attached an instruction.

The block goes onto the outgoing message. Review the selections in the attachments tree
below the chat input before sending.

## Multi-Snippet and Cross-File Support

You can annotate multiple disjoint ranges in a single file, and ranges across different
files. Each annotation is stored independently as a `(file, start_line, end_line,
annotation)` tuple. Navigate to another file by pressing `esc` to exit select mode, then
use tree navigation and re-enter select mode with `s`.
