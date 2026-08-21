package utils

import (
	"os"
	"strings"
)

// IsRunningInContainer detects if the CLI is running inside a container
// Checks multiple indicators to reliably detect containerized environments
func IsRunningInContainer() bool {
	if os.Getenv("INFER_IN_CONTAINER") == "true" {
		return true
	}

	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}

	if data, err := os.ReadFile("/proc/self/cgroup"); err == nil {
		content := string(data)
		if strings.Contains(content, "docker") ||
			strings.Contains(content, "kubepods") ||
			strings.Contains(content, "containerd") {
			return true
		}
	}

	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		return true
	}

	return false
}
