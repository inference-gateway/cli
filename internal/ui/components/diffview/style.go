package diffview

import (
	"github.com/charmbracelet/lipgloss"
)

// LineStyle holds the lipgloss styles for a single line type's three regions:
// the gutter line-number cell, the +/- symbol cell, and the code body.
type LineStyle struct {
	LineNumber lipgloss.Style
	Symbol     lipgloss.Style
	Code       lipgloss.Style
}

// Style aggregates LineStyles for every diff line kind.
type Style struct {
	DividerLine LineStyle // hunk header @@ ... @@
	MissingLine LineStyle // empty paired side in split layout
	EqualLine   LineStyle // unchanged / context
	InsertLine  LineStyle // additions
	DeleteLine  LineStyle // deletions
	Filename    LineStyle // file header above first hunk
}

// hex colors chosen to read well on the dark backgrounds used by Tokyo Night
// and Dracula. They are inspired by GitHub's dark diff palette: subtly tinted
// row backgrounds, brighter symbol-cell backgrounds, full-opacity FG.
const (
	darkBg          = "#1a1b26"
	darkFg          = "#a9b1d6"
	darkDim         = "#565f89"
	darkGutterBg    = "#1f2030"
	darkHunkFg      = "#7aa2f7"
	darkHunkBg      = "#24283b"
	darkInsertFg    = "#c4f0a0"
	darkInsertBg    = "#1e2e1e"
	darkInsertSymBg = "#2c4220"
	darkInsertNumBg = "#24351c"
	darkDeleteFg    = "#ffb4c0"
	darkDeleteBg    = "#2e1e1e"
	darkDeleteSymBg = "#4a2028"
	darkDeleteNumBg = "#35202a"
	darkFileFg      = "#c0caf5"
	darkFileBg      = "#283449"
)

const (
	lightBg          = "#ffffff"
	lightFg          = "#24292e"
	lightDim         = "#6e7781"
	lightGutterBg    = "#f6f8fa"
	lightHunkFg      = "#0550ae"
	lightHunkBg      = "#ddf4ff"
	lightInsertFg    = "#1a7f37"
	lightInsertBg    = "#dafbe1"
	lightInsertSymBg = "#aceebb"
	lightInsertNumBg = "#ccffd8"
	lightDeleteFg    = "#cf222e"
	lightDeleteBg    = "#ffebe9"
	lightDeleteSymBg = "#ffcecb"
	lightDeleteNumBg = "#ffd8d3"
	lightFileFg      = "#1f2328"
	lightFileBg      = "#eaeef2"
)

// DefaultDarkStyle returns a diff Style tuned for dark terminal themes.
func DefaultDarkStyle() Style {
	return buildStyle(styleParts{
		bg: darkBg, fg: darkFg, dim: darkDim,
		gutterBg:    darkGutterBg,
		hunkFg:      darkHunkFg,
		hunkBg:      darkHunkBg,
		insertFg:    darkInsertFg,
		insertBg:    darkInsertBg,
		insertSymBg: darkInsertSymBg,
		insertNumBg: darkInsertNumBg,
		deleteFg:    darkDeleteFg,
		deleteBg:    darkDeleteBg,
		deleteSymBg: darkDeleteSymBg,
		deleteNumBg: darkDeleteNumBg,
		fileFg:      darkFileFg,
		fileBg:      darkFileBg,
	})
}

// DefaultLightStyle returns a diff Style tuned for light terminal themes.
func DefaultLightStyle() Style {
	return buildStyle(styleParts{
		bg: lightBg, fg: lightFg, dim: lightDim,
		gutterBg:    lightGutterBg,
		hunkFg:      lightHunkFg,
		hunkBg:      lightHunkBg,
		insertFg:    lightInsertFg,
		insertBg:    lightInsertBg,
		insertSymBg: lightInsertSymBg,
		insertNumBg: lightInsertNumBg,
		deleteFg:    lightDeleteFg,
		deleteBg:    lightDeleteBg,
		deleteSymBg: lightDeleteSymBg,
		deleteNumBg: lightDeleteNumBg,
		fileFg:      lightFileFg,
		fileBg:      lightFileBg,
	})
}

type styleParts struct {
	bg, fg, dim                                  string
	gutterBg                                     string
	hunkFg, hunkBg                               string
	insertFg, insertBg, insertSymBg, insertNumBg string
	deleteFg, deleteBg, deleteSymBg, deleteNumBg string
	fileFg, fileBg                               string
}

func buildStyle(p styleParts) Style {
	return Style{
		DividerLine: LineStyle{
			LineNumber: lipgloss.NewStyle().Foreground(lipgloss.Color(p.dim)).Background(lipgloss.Color(p.hunkBg)),
			Code:       lipgloss.NewStyle().Foreground(lipgloss.Color(p.hunkFg)).Background(lipgloss.Color(p.hunkBg)).Bold(true),
		},
		MissingLine: LineStyle{
			LineNumber: lipgloss.NewStyle().Background(lipgloss.Color(p.gutterBg)),
			Code:       lipgloss.NewStyle().Background(lipgloss.Color(p.gutterBg)),
		},
		EqualLine: LineStyle{
			LineNumber: lipgloss.NewStyle().Foreground(lipgloss.Color(p.dim)).Background(lipgloss.Color(p.gutterBg)),
			Code:       lipgloss.NewStyle().Foreground(lipgloss.Color(p.fg)).Background(lipgloss.Color(p.bg)),
		},
		InsertLine: LineStyle{
			LineNumber: lipgloss.NewStyle().Foreground(lipgloss.Color(p.insertFg)).Background(lipgloss.Color(p.insertNumBg)),
			Symbol:     lipgloss.NewStyle().Foreground(lipgloss.Color(p.insertFg)).Background(lipgloss.Color(p.insertSymBg)).Bold(true),
			Code:       lipgloss.NewStyle().Foreground(lipgloss.Color(p.fg)).Background(lipgloss.Color(p.insertBg)),
		},
		DeleteLine: LineStyle{
			LineNumber: lipgloss.NewStyle().Foreground(lipgloss.Color(p.deleteFg)).Background(lipgloss.Color(p.deleteNumBg)),
			Symbol:     lipgloss.NewStyle().Foreground(lipgloss.Color(p.deleteFg)).Background(lipgloss.Color(p.deleteSymBg)).Bold(true),
			Code:       lipgloss.NewStyle().Foreground(lipgloss.Color(p.fg)).Background(lipgloss.Color(p.deleteBg)),
		},
		Filename: LineStyle{
			LineNumber: lipgloss.NewStyle().Foreground(lipgloss.Color(p.dim)).Background(lipgloss.Color(p.fileBg)),
			Code:       lipgloss.NewStyle().Foreground(lipgloss.Color(p.fileFg)).Background(lipgloss.Color(p.fileBg)).Bold(true),
		},
	}
}
