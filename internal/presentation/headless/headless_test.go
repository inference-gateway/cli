package headless

import (
	"errors"
	"strings"
	"testing"
)

func TestEmitPreRunError(t *testing.T) {
	tests := []struct {
		format string
		want   string
	}{
		{"json", `"agent_error"`},
		{"json-pretty", `"agent_error"`},
		{"ag-ui", `"RUN_ERROR"`},
		{"text", ""},
	}
	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			var out strings.Builder
			emitPreRunError(&out, tt.format, errors.New("gateway down"))
			if tt.want == "" {
				if out.Len() != 0 {
					t.Fatalf("text format must stay silent, got %q", out.String())
				}
				return
			}
			if !strings.Contains(out.String(), tt.want) || !strings.Contains(out.String(), "gateway down") {
				t.Fatalf("emitPreRunError(%s) output = %q, want %s with the message", tt.format, out.String(), tt.want)
			}
		})
	}
}
