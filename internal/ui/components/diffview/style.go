package diffview

import (
	"charm.land/lipgloss/v2"
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

// ThemePalette carries the semantic colours a diff Style derives from the
// active application theme. Callers build it from their theme so the diffview
// package stays decoupled from the app's theme types. Empty colour fields keep
// the tuned base value.
type ThemePalette struct {
	Add    string // additions foreground (+ symbol and line number)
	Remove string // deletions foreground (- symbol and line number)
	Accent string // hunk header (@@ ... @@) foreground
	Dim    string // gutter / context line numbers
	Dark   bool   // pick the dark vs light tuned background base
}

// NewThemeAwareStyle builds a diff Style whose semantic foreground colours come
// from the active theme, while the carefully-tuned background tints are
// inherited from the dark or light base style. This lets every theme show its
// own add/remove/accent colours without re-tuning the row backgrounds (which
// are what keep additions and deletions legible and colour-blind distinct).
func NewThemeAwareStyle(p ThemePalette) Style {
	base := DefaultLightStyle()
	if p.Dark {
		base = DefaultDarkStyle()
	}

	if p.Add != "" {
		add := lipgloss.Color(p.Add)
		base.InsertLine.LineNumber = base.InsertLine.LineNumber.Foreground(add)
		base.InsertLine.Symbol = base.InsertLine.Symbol.Foreground(add)
	}
	if p.Remove != "" {
		remove := lipgloss.Color(p.Remove)
		base.DeleteLine.LineNumber = base.DeleteLine.LineNumber.Foreground(remove)
		base.DeleteLine.Symbol = base.DeleteLine.Symbol.Foreground(remove)
	}
	if p.Accent != "" {
		base.DividerLine.Code = base.DividerLine.Code.Foreground(lipgloss.Color(p.Accent))
	}
	if p.Dim != "" {
		dim := lipgloss.Color(p.Dim)
		base.DividerLine.LineNumber = base.DividerLine.LineNumber.Foreground(dim)
		base.EqualLine.LineNumber = base.EqualLine.LineNumber.Foreground(dim)
	}

	return base
}
