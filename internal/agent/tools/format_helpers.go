package tools

// Helpers for reading values out of a domain.ToolExecutionResult.Data map inside
// FormatResult/FormatPreview implementations. Data is typed `any` and is persisted
// and reloaded through JSON (see internal/infra/storage), so on reload every number
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
