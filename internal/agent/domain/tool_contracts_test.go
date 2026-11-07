package domain

import "testing"

func TestRequireStringArg(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		key     string
		wantVal string
		wantErr string
	}{
		{"valid string", map[string]any{"selector": "button"}, "selector", "button", ""},
		{"missing key", map[string]any{}, "selector", "", "selector is required and must be a non-empty string"},
		{"wrong type", map[string]any{"selector": 42}, "selector", "", "selector is required and must be a non-empty string"},
		{"nil value", map[string]any{"selector": nil}, "selector", "", "selector is required and must be a non-empty string"},
		{"empty string", map[string]any{"selector": ""}, "selector", "", "selector is required and must be a non-empty string"},
		{"whitespace only", map[string]any{"selector": "   "}, "selector", "", "selector is required and must be a non-empty string"},
		{"nil args", nil, "selector", "", "selector is required and must be a non-empty string"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RequireStringArg(tt.args, tt.key)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("RequireStringArg() err = %v, want nil", err)
				}
			} else if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("RequireStringArg() err = %v, want %q", err, tt.wantErr)
			}
			if got != tt.wantVal {
				t.Errorf("RequireStringArg() = %q, want %q", got, tt.wantVal)
			}
		})
	}
}
