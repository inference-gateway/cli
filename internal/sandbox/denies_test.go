package sandbox

import (
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestDeniesMatchesTheDefaultPolicyAgainstProjectFiles(t *testing.T) {
	cfg := config.DefaultConfig()
	project := t.TempDir()
	tests := []struct {
		rel  string
		want bool
	}{
		{".env", true},
		{"deploy/.env.production", true},
		{".env.example", false},
		{"certs/server.key", true},
		{"certs/ca.pem", true},
		{".infer/config.yaml", true},
		{".git/config", true},
		{"src/main.go", false},
		{"notes/todo.txt", false},
	}
	for _, tt := range tests {
		if got := Denies(cfg, project, filepath.Join(project, tt.rel)); got != tt.want {
			t.Fatalf("Denies(%s) = %v, want %v", tt.rel, got, tt.want)
		}
	}
}
