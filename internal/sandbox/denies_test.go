package sandbox

import (
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
	sandboxdomain "github.com/inference-gateway/cli/internal/sandbox/domain"
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

func TestBlockingDenials(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.Sandbox.Filesystem.Denied = []sandboxdomain.Denied{
		{Path: "auth.yaml"},
		{Path: ".git/"},
		{Path: "asks-first", OnViolation: sandboxdomain.ViolationApproval},
	}

	denied := BlockingDenials(cfg)
	if len(denied) != 2 || denied[0].Path != "auth.yaml" || denied[1].Path != ".git/" {
		t.Fatalf("BlockingDenials() = %+v, want the two blocking entries in order", denied)
	}
}
