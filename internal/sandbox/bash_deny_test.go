package sandbox

import (
	"os"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestBashCommandDeniedPath(t *testing.T) {
	cfg := config.DefaultConfig()
	home := t.TempDir()
	t.Setenv("HOME", home)

	authPath := filepath.Join(home, ".infer", "auth.yaml")
	envPath := filepath.Join(home, "deploy", ".env.production")
	for _, dir := range []string{filepath.Dir(authPath), filepath.Dir(envPath)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	files := map[string]string{
		authPath: "mocked",
		envPath:  "mocked",
		filepath.Join(home, ".infer", "sandbox.yaml"): "mocked",
		filepath.Join(home, ".env.example"):           "example",
		filepath.Join(home, "notes.md"):               "notes",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	tests := []struct {
		name    string
		command string
		want    string
	}{
		{"denied auth file", "cat ~/.infer/auth.yaml", authPath},
		{"denied env pattern", "cat ~/deploy/.env.production", envPath},
		{"denied in a chain", "echo first && cat ~/.infer/auth.yaml", authPath},
		{"denied through a pipe", "cat ~/.infer/auth.yaml | wc -l", authPath},
		{"denied via input redirect", "wc -l < ~/.infer/auth.yaml", authPath},
		{"denied as a link source", "ln -s ~/.infer/auth.yaml ~/leak", authPath},
		{"denied through a glob", "cat ~/.infer/*.yaml", authPath},
		{"excepted env example", "cat ~/.env.example", ""},
		{"outside but not denied", "cat ~/notes.md", ""},
		{"approval class stays with the gate", "cat ~/.infer/sandbox.yaml", ""},
		{"print-only command", "echo ~/.infer/auth.yaml", ""},
		{"command substitution is the kernel layer's", "cat $(cat ~/.infer/auth.yaml)", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, denied := BashCommandDeniedPath(cfg, tt.command)
			if denied != (tt.want != "") || (denied && path != tt.want) {
				t.Fatalf("BashCommandDeniedPath(%q) = (%q, %v), want (%q, %v)",
					tt.command, path, denied, tt.want, tt.want != "")
			}
		})
	}
}
