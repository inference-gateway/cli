package components

import (
	"strings"
	"testing"
)

func TestDiffRenderer_RenderDiff(t *testing.T) {
	renderer := NewDiffRenderer(nil)

	t.Run("New file creation", func(t *testing.T) {
		diffInfo := DiffInfo{
			FilePath:   "test.go",
			OldContent: "",
			NewContent: "package main\n\nfunc main() {}",
			Title:      "Test Diff",
		}

		output := renderer.RenderDiff(diffInfo)

		if !strings.Contains(output, "@@ -0,0 +1,") {
			t.Errorf("Expected new file chunk header, got:\n%s", output)
		}

		if !strings.Contains(output, "+package main") {
			t.Errorf("Expected added lines with + prefix, got:\n%s", output)
		}
	})

	t.Run("File deletion", func(t *testing.T) {
		diffInfo := DiffInfo{
			FilePath:   "test.go",
			OldContent: "package main\n\nfunc main() {}",
			NewContent: "",
			Title:      "Test Diff",
		}

		output := renderer.RenderDiff(diffInfo)

		if !strings.Contains(output, "@@ -1,") && !strings.Contains(output, " +0,0 @@") {
			t.Errorf("Expected deletion chunk header, got:\n%s", output)
		}

		if !strings.Contains(output, "-package main") {
			t.Errorf("Expected deleted lines with - prefix, got:\n%s", output)
		}
	})

	t.Run("File modification", func(t *testing.T) {
		diffInfo := DiffInfo{
			FilePath:   "test.go",
			OldContent: "Hello World",
			NewContent: "Hello Universe",
			Title:      "Test Diff",
		}

		output := renderer.RenderDiff(diffInfo)

		if !strings.Contains(output, "@@") {
			t.Errorf("Expected chunk header, got:\n%s", output)
		}

		if !strings.Contains(output, "-Hello World") {
			t.Errorf("Expected old content with - prefix, got:\n%s", output)
		}
		if !strings.Contains(output, "+Hello Universe") {
			t.Errorf("Expected new content with + prefix, got:\n%s", output)
		}
	})

	t.Run("No changes", func(t *testing.T) {
		diffInfo := DiffInfo{
			FilePath:   "test.go",
			OldContent: "Same content",
			NewContent: "Same content",
			Title:      "Test Diff",
		}

		output := renderer.RenderDiff(diffInfo)

		if !strings.Contains(output, "test.go") {
			t.Errorf("Expected file path in output, got:\n%s", output)
		}

		if strings.Contains(output, "-Same content") || strings.Contains(output, "+Same content") {
			t.Errorf("Should not show diff for identical content, got:\n%s", output)
		}
	})
}

func TestDiffRenderer_RenderMultiEditToolArguments(t *testing.T) {
	renderer := NewDiffRenderer(nil)

	t.Run("Multiple edits", func(t *testing.T) {
		args := map[string]any{
			"file_path": "/path/to/test.go",
			"edits": []any{
				map[string]any{
					"old_string": "Hello",
					"new_string": "Hi",
				},
				map[string]any{
					"old_string":  "World",
					"new_string":  "Universe",
					"replace_all": true,
				},
			},
		}

		output := renderer.RenderMultiEditToolArguments(args)

		if !strings.Contains(output, "test.go") {
			t.Errorf("Expected file name in output, got:\n%s", output)
		}

		if !strings.Contains(output, "2 edits") {
			t.Errorf("Expected '2 edits' in output, got:\n%s", output)
		}

		if !strings.Contains(output, "Edit 1:") {
			t.Errorf("Expected 'Edit 1:' in output, got:\n%s", output)
		}
		if !strings.Contains(output, "Edit 2:") {
			t.Errorf("Expected 'Edit 2:' in output, got:\n%s", output)
		}

		if !strings.Contains(output, "Replace all") {
			t.Errorf("Expected 'Replace all' for second edit, got:\n%s", output)
		}

		if !strings.Contains(output, "-Hello") || !strings.Contains(output, "+Hi") {
			t.Errorf("Expected diff for first edit, got:\n%s", output)
		}
	})

	t.Run("Empty edits array", func(t *testing.T) {
		args := map[string]any{
			"file_path": "/path/to/test.go",
			"edits":     []any{},
		}

		output := renderer.RenderMultiEditToolArguments(args)

		if !strings.Contains(output, "test.go") {
			t.Errorf("Expected file name in output, got:\n%s", output)
		}

		if !strings.Contains(output, "0 edits") {
			t.Errorf("Expected '0 edits' in output, got:\n%s", output)
		}
	})

	t.Run("Invalid edits format", func(t *testing.T) {
		args := map[string]any{
			"file_path": "/path/to/test.go",
			"edits":     "invalid",
		}

		output := renderer.RenderMultiEditToolArguments(args)

		if !strings.Contains(output, "Invalid edits format") {
			t.Errorf("Expected error message for invalid format, got:\n%s", output)
		}
	})
}

func TestDiffRenderer_HelperMethods(t *testing.T) {
	renderer := NewDiffRenderer(nil)

	t.Run("renderNewFileContent", func(t *testing.T) {
		content := "line1\nline2\nline3"
		output := renderer.renderNewFileContent(content)

		if !strings.Contains(output, "@@ -0,0 +1,3 @@") {
			t.Errorf("Expected chunk header for 3 lines, got:\n%s", output)
		}

		if !strings.Contains(output, "+line1") {
			t.Errorf("Expected +line1, got:\n%s", output)
		}
		if !strings.Contains(output, "+line2") {
			t.Errorf("Expected +line2, got:\n%s", output)
		}
		if !strings.Contains(output, "+line3") {
			t.Errorf("Expected +line3, got:\n%s", output)
		}
	})

	t.Run("renderDeletedFileContent", func(t *testing.T) {
		content := "line1\nline2"
		output := renderer.renderDeletedFileContent(content)

		if !strings.Contains(output, "@@ -1,2 +0,0 @@") {
			t.Errorf("Expected chunk header for deletion, got:\n%s", output)
		}

		if !strings.Contains(output, "-line1") {
			t.Errorf("Expected -line1, got:\n%s", output)
		}
		if !strings.Contains(output, "-line2") {
			t.Errorf("Expected -line2, got:\n%s", output)
		}
	})

	t.Run("renderUnifiedDiff", func(t *testing.T) {
		oldContent := "Hello\nWorld"
		newContent := "Hello\nUniverse"
		output := renderer.renderUnifiedDiff(oldContent, newContent, 1)

		if !strings.Contains(output, "@@") {
			t.Errorf("Expected chunk header, got:\n%s", output)
		}

		if !strings.Contains(output, " Hello") {
			t.Errorf("Expected context line ' Hello', got:\n%s", output)
		}

		if !strings.Contains(output, "-World") {
			t.Errorf("Expected -World, got:\n%s", output)
		}
		if !strings.Contains(output, "+Universe") {
			t.Errorf("Expected +Universe, got:\n%s", output)
		}
	})

	t.Run("renderUnifiedDiff with identical content", func(t *testing.T) {
		content := "Same\nContent"
		output := renderer.renderUnifiedDiff(content, content, 1)

		if output != "" {
			t.Errorf("Expected empty string for identical content, got:\n%s", output)
		}
	})
}

func TestDiffRenderer_Styling(t *testing.T) {
	renderer := NewDiffRenderer(nil)

	testContent := "test"

	_ = renderer.additionStyle.Render(testContent)
	_ = renderer.deletionStyle.Render(testContent)
	_ = renderer.headerStyle.Render(testContent)
	_ = renderer.fileStyle.Render(testContent)
	_ = renderer.contextStyle.Render(testContent)
	_ = renderer.chunkStyle.Render(testContent)
}

func TestNewToolDiffRenderer(t *testing.T) {
	renderer := NewToolDiffRenderer()

	if renderer == nil {
		t.Fatal("NewToolDiffRenderer should not return nil")
	}

	diffInfo := DiffInfo{
		FilePath:   "test.go",
		OldContent: "old",
		NewContent: "new",
		Title:      "Test",
	}

	output := renderer.RenderDiff(diffInfo)
	if output == "" {
		t.Error("Renderer should produce output")
	}
}
