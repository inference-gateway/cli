package utils

import (
	"os"
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
			// Save original env vars
			origInferContainer := os.Getenv("INFER_IN_CONTAINER")
			origK8s := os.Getenv("KUBERNETES_SERVICE_HOST")
			defer func() {
				os.Setenv("INFER_IN_CONTAINER", origInferContainer)
				os.Setenv("KUBERNETES_SERVICE_HOST", origK8s)
			}()

			// Clear all container-related env vars
			os.Unsetenv("INFER_IN_CONTAINER")
			os.Unsetenv("KUBERNETES_SERVICE_HOST")

			// Set the test env var
			if tt.envVar != "" && tt.envValue != "" {
				os.Setenv(tt.envVar, tt.envValue)
			}

			got := IsRunningInContainer()
			if got != tt.want {
				t.Errorf("IsRunningInContainer() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsRunningInContainer_NoEnvVars(t *testing.T) {
	// Save original env vars
	origInferContainer := os.Getenv("INFER_IN_CONTAINER")
	origK8s := os.Getenv("KUBERNETES_SERVICE_HOST")
	defer func() {
		os.Setenv("INFER_IN_CONTAINER", origInferContainer)
		os.Setenv("KUBERNETES_SERVICE_HOST", origK8s)
	}()

	// Clear all container-related env vars
	os.Unsetenv("INFER_IN_CONTAINER")
	os.Unsetenv("KUBERNETES_SERVICE_HOST")

	// Note: This test will return false on most systems unless running in an actual container
	// We're mainly testing that the function doesn't panic without env vars
	result := IsRunningInContainer()
	_ = result
}
