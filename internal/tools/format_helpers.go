package tools

import (
	"fmt"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// Helpers for reading values out of a agentdomain.ToolExecutionResult.Data map inside
// FormatResult/FormatPreview implementations. Data is typed `any` and is persisted
// and reloaded through JSON (see internal/platform/storage), so on reload every number
// becomes float64 and every []map[string]any becomes []any of map[string]any. Bare
// type assertions (e.g. v.(int)) panic in that case and crash the TUI, so format
// methods that read numeric or slice fields must coerce through these instead.

// toInt coerces a numeric Data value to int, accepting both the freshly-produced Go
// types (int, int64, …) and the float64 a JSON round-trip yields. Returns 0 for nil
// or non-numeric values.
func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case int32:
		return int(n)
	case float64:
		return int(n)
	case float32:
		return int(n)
	case *int:
		if n != nil {
			return *n
		}
	}
	return 0
}

// asMapSlice normalizes a slice-of-maps Data field that is []map[string]any when
// freshly produced but []any (each element a map[string]any) after a JSON round-trip.
// Returns nil for anything else.
func asMapSlice(v any) []map[string]any {
	switch s := v.(type) {
	case []map[string]any:
		return s
	case []any:
		out := make([]map[string]any, 0, len(s))
		for _, e := range s {
			if m, ok := e.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

// resultFormatter renders one view of a tool result.
type resultFormatter func(*agentdomain.ToolExecutionResult) string

// formatResult routes a result to the tool's formatter for the requested view.
// An unknown view gets the UI formatter.
func formatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType, ui, llm, preview resultFormatter) string {
	switch formatType {
	case agentdomain.FormatterLLM:
		return llm(result)
	case agentdomain.FormatterShort:
		return preview(result)
	default:
		return ui(result)
	}
}

// savedFile describes a media tool whose result is the path of the one file it
// saved. details renders the extra fields of the LLM summary, or is nil.
type savedFile struct {
	tool    string
	noun    string
	failure string
	done    string
	details func(data map[string]any) string
}

// formatSavedResult formats a saved-file result: the preview names the path,
// every other view expands the result with the full summary.
func formatSavedResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType, file savedFile) string {
	if formatType == agentdomain.FormatterShort {
		return file.preview(result)
	}
	return file.forLLM(result)
}

func (f savedFile) preview(result *agentdomain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return f.failure
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return f.done
	}
	path, _ := data["path"].(string)
	return fmt.Sprintf("%s saved to %s", f.noun, path)
}

func (f savedFile) forLLM(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Error: no result"
	}
	if !result.Success {
		return fmt.Sprintf("%s: %s", f.failure, result.Error)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return f.done
	}
	path, _ := data["path"].(string)
	summary := fmt.Sprintf("%s saved to %s", f.noun, path)
	if f.details != nil {
		summary += f.details(data)
	}
	return NewBaseFormatter(f.tool).FormatExpanded(result, summary)
}

// imageDetails renders the quality and size an image tool reports.
func imageDetails(data map[string]any) string {
	quality, _ := data["quality"].(string)
	size, _ := data["size"].(string)
	return fmt.Sprintf(" (quality: %s, size: %s)", quality, size)
}
