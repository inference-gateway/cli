package utils

import (
	"testing"
)

func TestIsRunningInContainer(t *testing.T) {
	tests := []struct {
		name     string
		envVar   string
		envValue string
		want     bool
	}{
		{
			name:     "explicit container env var set to true",
			envVar:   "INFER_IN_CONTAINER",
			envValue: "true",
			want:     true,
		},
		{
			name:     "explicit container env var set to false",
			envVar:   "INFER_IN_CONTAINER",
			envValue: "false",
			want:     false,
		},
		{
			name:     "kubernetes environment",
			envVar:   "KUBERNETES_SERVICE_HOST",
			envValue: "10.0.0.1",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("INFER_IN_CONTAINER", "")
			t.Setenv("KUBERNETES_SERVICE_HOST", "")

			if tt.envVar != "" && tt.envValue != "" {
				t.Setenv(tt.envVar, tt.envValue)
			}

			got := IsRunningInContainer()
			if got != tt.want {
				t.Errorf("IsRunningInContainer() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsRunningInContainer_NoEnvVars(t *testing.T) {
	t.Setenv("INFER_IN_CONTAINER", "")
	t.Setenv("KUBERNETES_SERVICE_HOST", "")

	result := IsRunningInContainer()
	_ = result
}
