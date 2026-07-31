package shortcuts

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/inference-gateway/sdk"

	mocksdk "github.com/inference-gateway/cli/tests/mocks/sdk"
)

func TestImageShortcut_GetName(t *testing.T) {
	shortcut := NewImageShortcut(&mocksdk.FakeClient{}, &mockModelService{})
	if got := shortcut.GetName(); got != "image" {
		t.Errorf("GetName() = %q, want 'image'", got)
	}
}

func TestImageShortcut_GetUsage(t *testing.T) {
	shortcut := NewImageShortcut(&mocksdk.FakeClient{}, &mockModelService{})
	if got := shortcut.GetUsage(); got != "/image <prompt>" {
		t.Errorf("GetUsage() = %q, want '/image <prompt>'", got)
	}
}

func TestImageShortcut_CanExecute(t *testing.T) {
	shortcut := NewImageShortcut(&mocksdk.FakeClient{}, &mockModelService{})

	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "no args", args: nil, want: false},
		{name: "single word prompt", args: []string{"cat"}, want: true},
		{name: "multi word prompt", args: []string{"a", "cat", "in", "space"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shortcut.CanExecute(tt.args); got != tt.want {
				t.Errorf("CanExecute(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestImageShortcut_Execute_SuccessBase64(t *testing.T) {
	imgData := []byte("fake-png-bytes")
	encoded := base64.StdEncoding.EncodeToString(imgData)

	fake := &mocksdk.FakeClient{}
	fake.CreateImageReturns(&sdk.ImagesResponse{Data: []sdk.Image{{B64Json: &encoded}}}, nil)

	shortcut := NewImageShortcut(fake, &mockModelService{currentModel: "openai/gpt-image-1"})
	t.Chdir(t.TempDir())

	result, err := shortcut.Execute(context.Background(), []string{"a", "cat"})
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() success = false, want true (output %q)", result.Output)
	}
	if !strings.Contains(result.Output, "image-") || !strings.Contains(result.Output, ".png") {
		t.Errorf("Execute() output = %q, want it to mention the saved .png path", result.Output)
	}

	files, err := filepath.Glob("image-*.png")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected exactly one saved image, got %v", files)
	}
	got, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(imgData) {
		t.Errorf("saved image = %q, want %q", got, imgData)
	}

	// The request carries the session's provider, model, and the full prompt.
	ctx, provider, req := fake.CreateImageArgsForCall(0)
	if ctx == nil {
		t.Error("request context is nil")
	}
	if provider != sdk.Provider("openai") {
		t.Errorf("provider = %q, want openai", provider)
	}
	if req.Model == nil || *req.Model != "gpt-image-1" {
		t.Errorf("request model = %v, want gpt-image-1", req.Model)
	}
	if req.Prompt != "a cat" {
		t.Errorf("request prompt = %q, want %q", req.Prompt, "a cat")
	}
}

func TestImageShortcut_Execute_SuccessURL(t *testing.T) {
	imgData := []byte("fake-png-bytes")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(imgData)
	}))
	defer server.Close()

	fake := &mocksdk.FakeClient{}
	fake.CreateImageReturns(&sdk.ImagesResponse{Data: []sdk.Image{{URL: &server.URL}}}, nil)

	shortcut := NewImageShortcut(fake, &mockModelService{currentModel: "openai/gpt-image-1"})
	shortcut.httpClient = server.Client()
	t.Chdir(t.TempDir())

	result, err := shortcut.Execute(context.Background(), []string{"cat"})
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() success = false, want true (output %q)", result.Output)
	}

	files, err := filepath.Glob("image-*.png")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected exactly one saved image, got %v", files)
	}
	got, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(imgData) {
		t.Errorf("saved image = %q, want %q", got, imgData)
	}
}

func TestImageShortcut_Execute_URLDownloadError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	fake := &mocksdk.FakeClient{}
	fake.CreateImageReturns(&sdk.ImagesResponse{Data: []sdk.Image{{URL: &server.URL}}}, nil)

	shortcut := NewImageShortcut(fake, &mockModelService{currentModel: "openai/gpt-image-1"})
	shortcut.httpClient = server.Client()

	result, err := shortcut.Execute(context.Background(), []string{"cat"})
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	if result.Success {
		t.Errorf("Execute() success = true, want false")
	}
	if !strings.Contains(result.Output, "500") {
		t.Errorf("Execute() output = %q, want it to mention the download status", result.Output)
	}
}

func TestImageShortcut_Execute_ClientError(t *testing.T) {
	fake := &mocksdk.FakeClient{}
	fake.CreateImageReturns(nil, errors.New("unsupported provider"))

	shortcut := NewImageShortcut(fake, &mockModelService{currentModel: "openai/gpt-image-1"})

	result, err := shortcut.Execute(context.Background(), []string{"cat"})
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	if result.Success {
		t.Errorf("Execute() success = true, want false")
	}
	if !strings.Contains(result.Output, "unsupported provider") {
		t.Errorf("Execute() output = %q, want it to surface the gateway error", result.Output)
	}
}

func TestImageShortcut_Execute_NoModel(t *testing.T) {
	shortcut := NewImageShortcut(&mocksdk.FakeClient{}, &mockModelService{})

	result, err := shortcut.Execute(context.Background(), []string{"cat"})
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	if result.Success {
		t.Errorf("Execute() success = true, want false")
	}
	if !strings.Contains(result.Output, "model") {
		t.Errorf("Execute() output = %q, want it to mention selecting a model", result.Output)
	}
}

func TestImageShortcut_Execute_InvalidModel(t *testing.T) {
	fake := &mocksdk.FakeClient{}
	shortcut := NewImageShortcut(fake, &mockModelService{currentModel: "gpt-image-1"})

	result, err := shortcut.Execute(context.Background(), []string{"cat"})
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	if result.Success {
		t.Errorf("Execute() success = true, want false")
	}
	if !strings.Contains(result.Output, "provider/model") {
		t.Errorf("Execute() output = %q, want it to mention the provider/model format", result.Output)
	}
	if fake.CreateImageCallCount() != 0 {
		t.Errorf("CreateImage was called %d times, want 0", fake.CreateImageCallCount())
	}
}

func TestImageShortcut_Execute_NoData(t *testing.T) {
	fake := &mocksdk.FakeClient{}
	fake.CreateImageReturns(&sdk.ImagesResponse{}, nil)

	shortcut := NewImageShortcut(fake, &mockModelService{currentModel: "openai/gpt-image-1"})

	result, err := shortcut.Execute(context.Background(), []string{"cat"})
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	if result.Success {
		t.Errorf("Execute() success = true, want false")
	}
	if !strings.Contains(result.Output, "no images") {
		t.Errorf("Execute() output = %q, want it to mention the empty response", result.Output)
	}
}
