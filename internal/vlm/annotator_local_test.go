package vlm

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

func notFound(string) (string, error) { return "", errors.New("not found") }

func localTestConfig(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	for _, f := range []string{"Qwen3VL-2B-Instruct-Q4_K_M.gguf", "mmproj-Qwen3VL-2B-Instruct-F16.gguf"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("m"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.DefaultConfig()
	cfg.Vision.Annotator.Enabled = true
	cfg.Vision.Annotator.ModelsDir = dir
	cfg.Prompts = *config.DefaultPromptsConfig()
	return cfg
}

func TestLocalAnnotateImageSuccess(t *testing.T) {
	cfg := localTestConfig(t)
	a := NewLocalAnnotator(cfg)
	a.lookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }

	var gotArgs []string
	a.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		gotArgs = args
		return []byte(`{"summary":"Login form","elements":[{"index":1,"label":"button","text":"Sign in","bbox":[0,0,1000,1000]}]}`), nil
	}

	img := domain.ImageAttachment{Data: base64.StdEncoding.EncodeToString([]byte("img")), MimeType: "image/png"}
	got, err := a.AnnotateImage(context.Background(), img, domain.AnnotateOptions{Prompt: "describe UI", Width: 1024, Height: 768})
	if err != nil {
		t.Fatalf("AnnotateImage: %v", err)
	}
	if got.Summary != "Login form" {
		t.Errorf("Summary = %q", got.Summary)
	}
	if want := [4]int{0, 0, 1024, 768}; got.Elements[0].BBox != want {
		t.Errorf("BBox = %v, want %v (rescaled from 0-1000)", got.Elements[0].BBox, want)
	}

	joined := strings.Join(gotArgs, " ")
	if !strings.Contains(joined, "--mmproj") || !strings.Contains(joined, "--image") {
		t.Errorf("expected mmproj and image args, got %q", joined)
	}
	if !strings.Contains(joined, "describe UI") {
		t.Errorf("expected task prompt in args, got %q", joined)
	}
}

func TestLocalAnnotateImageUsesSourcePath(t *testing.T) {
	cfg := localTestConfig(t)
	imgPath := filepath.Join(t.TempDir(), "frame.png")
	if err := os.WriteFile(imgPath, []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := NewLocalAnnotator(cfg)
	a.lookPath = func(name string) (string, error) { return name, nil }
	a.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		for i, arg := range args {
			if arg == "--image" {
				if args[i+1] != imgPath {
					t.Errorf("image arg = %q, want %q", args[i+1], imgPath)
				}
			}
		}
		return []byte(`{"summary":"ok"}`), nil
	}

	img := domain.ImageAttachment{SourcePath: imgPath, Data: base64.StdEncoding.EncodeToString([]byte("img")), MimeType: "image/png"}
	if _, err := a.AnnotateImage(context.Background(), img, domain.AnnotateOptions{}); err != nil {
		t.Fatalf("AnnotateImage: %v", err)
	}
}

func TestLocalAnnotateImageBinaryMissing(t *testing.T) {
	cfg := localTestConfig(t)
	a := NewLocalAnnotator(cfg)
	a.lookPath = notFound

	img := domain.ImageAttachment{Data: base64.StdEncoding.EncodeToString([]byte("img")), MimeType: "image/png"}
	_, err := a.AnnotateImage(context.Background(), img, domain.AnnotateOptions{})
	if err == nil || !strings.Contains(err.Error(), "llama-mtmd-cli not found") {
		t.Fatalf("expected actionable binary-missing error, got %v", err)
	}
}

func TestLocalResolveBinaryConfiguredPath(t *testing.T) {
	cfg := localTestConfig(t)
	cfg.Vision.Annotator.BinaryPath = "/opt/llama-mtmd-cli"
	a := NewLocalAnnotator(cfg)
	a.lookPath = func(name string) (string, error) {
		if name == "/opt/llama-mtmd-cli" {
			return name, nil
		}
		return "", errors.New("not found")
	}
	got, err := a.resolveBinary(context.Background())
	if err != nil {
		t.Fatalf("resolveBinary: %v", err)
	}
	if got != "/opt/llama-mtmd-cli" {
		t.Errorf("resolveBinary = %q", got)
	}
}
