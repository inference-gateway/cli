package accessibility

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
)

const (
	testHelperEnvironment = "INFER_TEST_ACCESSIBILITY_HELPER"
	testHelperMode        = "INFER_TEST_ACCESSIBILITY_HELPER_MODE"
)

func TestSubprocessProviderElements(t *testing.T) {
	provider := newTestSubprocessProvider("success", 10*time.Second)
	elements, err := provider.Elements(context.Background(), "frontmost")
	if err != nil {
		t.Fatalf("Elements() error = %v", err)
	}
	if len(elements) != 1 || elements[0].Label != "Save" {
		t.Fatalf("Elements() = %+v, want one Save element", elements)
	}
}

func TestSubprocessProviderMapsHelperErrors(t *testing.T) {
	provider := newTestSubprocessProvider("permission", 10*time.Second)
	_, err := provider.Elements(context.Background(), "frontmost")
	if !errors.Is(err, ErrPermission) {
		t.Fatalf("Elements() error = %v, want ErrPermission", err)
	}
}

func TestSubprocessProviderIsolatesHelperFailure(t *testing.T) {
	provider := newTestSubprocessProvider("exit", 10*time.Second)
	_, err := provider.Elements(context.Background(), "frontmost")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Elements() error = %v, want ErrUnavailable", err)
	}
}

func TestSubprocessProviderTimesOutHelper(t *testing.T) {
	provider := newTestSubprocessProvider("hang", 20*time.Millisecond)
	_, err := provider.Elements(context.Background(), "frontmost")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Elements() error = %v, want ErrUnavailable", err)
	}
}

func TestRunHelperRejectsInvalidRequest(t *testing.T) {
	var output bytes.Buffer
	if err := RunHelper(strings.NewReader("not-json"), &output); err != nil {
		t.Fatalf("RunHelper() error = %v", err)
	}
	var resp response
	if err := json.Unmarshal(output.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Code != "unavailable" || resp.Error == "" {
		t.Fatalf("response = %+v, want unavailable error", resp)
	}
}

func newTestSubprocessProvider(mode string, timeout time.Duration) *subprocessProvider {
	return &subprocessProvider{
		timeout: timeout,
		command: func(ctx context.Context) (*exec.Cmd, error) {
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestAccessibilityHelperProcess")
			cmd.Env = append(os.Environ(), testHelperEnvironment+"=1", testHelperMode+"="+mode)
			return cmd, nil
		},
	}
}

func TestAccessibilityHelperProcess(t *testing.T) {
	if os.Getenv(testHelperEnvironment) != "1" {
		return
	}
	switch os.Getenv(testHelperMode) {
	case "exit":
		os.Exit(23)
	case "hang":
		time.Sleep(5 * time.Second)
	case "permission":
		_ = json.NewEncoder(os.Stdout).Encode(response{Code: "permission", Error: "permission denied"})
	default:
		var req request
		_ = json.NewDecoder(os.Stdin).Decode(&req)
		if req.Action == "elements" {
			_ = json.NewEncoder(os.Stdout).Encode(response{Elements: []computerdomain.UIElement{{
				Role: "button", Label: "Save", State: "enabled actions=press", BBox: [4]int{10, 20, 50, 40},
			}}})
			return
		}
		_ = json.NewEncoder(os.Stdout).Encode(response{})
	}
}
