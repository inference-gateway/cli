package shortcuts

import (
	"errors"
	"testing"

	require "github.com/stretchr/testify/require"
)

func TestReloadShortcut(t *testing.T) {
	tests := []struct {
		name    string
		reload  func() ([]string, []string, error)
		want    string
		wantErr string
	}{
		{"nothing changed", func() ([]string, []string, error) { return nil, nil, nil }, "No config changes", ""},
		{"applied only", func() ([]string, []string, error) {
			return []string{"gateway.timeout", "agent.model"}, nil, nil
		}, "Reloaded gateway.timeout, agent.model", ""},
		{"restart only", func() ([]string, []string, error) {
			return nil, []string{"tools.safety.approval_behaviour"}, nil
		}, "Restart to apply tools.safety.approval_behaviour", ""},
		{"both", func() ([]string, []string, error) {
			return []string{"gateway.timeout"}, []string{"chat.theme"}, nil
		}, "Reloaded gateway.timeout · restart to apply chat.theme", ""},
		{"reload fails", func() ([]string, []string, error) {
			return nil, nil, errors.New("invalid reasoning effort")
		}, "", "reloading config: invalid reasoning effort"},
		{"no reloader", nil, "", "only available in infer chat"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := NewReloadShortcut(tt.reload).Execute(t.Context(), nil)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, SideEffectReloadConfig, result.SideEffect)
			require.Equal(t, tt.want, result.Data)
		})
	}
}
