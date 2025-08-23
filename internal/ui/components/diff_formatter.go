package components

// DiffFormatter interface for tools that can provide diff information
type DiffFormatter interface {
	// GetDiffInfo returns the information needed to render a diff
	GetDiffInfo(args map[string]any) *DiffInfo
}

// DiffInfo contains all information needed to render a diff
type DiffInfo struct {
	Type       DiffType
	FilePath   string
	OldContent string
	NewContent string
	Title      string // Custom title for the diff section
}

// ToolDiffRenderer provides diff rendering functionality for tools
type ToolDiffRenderer struct {
	diffComponent *GitDiffComponent
}

// NewToolDiffRenderer creates a new tool diff renderer
func NewToolDiffRenderer() *ToolDiffRenderer {
	return &ToolDiffRenderer{
		diffComponent: NewGitDiffComponent(),
	}
}

// RenderDiff renders a diff based on DiffInfo
func (r *ToolDiffRenderer) RenderDiff(info *DiffInfo) string {
	if info == nil {
		return "No diff information available.\n"
	}

	switch info.Type {
	case DiffTypeNew:
		title := info.Title
		if title == "" {
			title = "← Content to be written →"
		}
		return r.diffComponent.RenderNewFileWithTitle(info.FilePath, info.NewContent, title)
	case DiffTypeEdit:
		return r.diffComponent.RenderDiff(info.OldContent, info.NewContent)
	case DiffTypeDelete:
		return r.diffComponent.RenderDeletedFile(info.FilePath, info.OldContent)
	default:
		return "Unknown diff type.\n"
	}
}

// SetDimensions sets the dimensions for the diff component
func (r *ToolDiffRenderer) SetDimensions(width, height int) {
	r.diffComponent.SetWidth(width)
	r.diffComponent.SetHeight(height)
}
