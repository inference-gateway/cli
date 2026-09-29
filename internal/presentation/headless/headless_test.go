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

func TestValidateOptions(t *testing.T) {
	tests := []struct {
		name    string
		opts    Options
		wantErr string
	}{
		{"one-shot task", Options{Task: "fix it", Format: "json"}, ""},
		{"serve worker", Options{Serve: true, Format: "ag-ui"}, ""},
		{"unknown format", Options{Task: "fix it", Format: "xml"}, "invalid --format"},
		{"serve with a task", Options{Serve: true, Task: "fix it", Format: "ag-ui"}, "--serve takes no task"},
		{"serve without ag-ui", Options{Serve: true, Format: "json"}, "needs --format ag-ui"},
		{"no task without serve", Options{Format: "json"}, "a task is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOptions(tt.opts)
			if tt.wantErr == "" && err != nil || tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("validateOptions(%+v) = %v, want %q", tt.opts, err, tt.wantErr)
			}
		})
	}
}
