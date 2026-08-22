package computer

import (
	"context"
	"errors"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
	accessibility "github.com/inference-gateway/cli/internal/computer/infrastructure/accessibility"
)

type fakeAccessibilityProvider struct {
	elements    []computerdomain.UIElement
	elementsErr error
	pressErr    error
	pressed     string
}

func (f *fakeAccessibilityProvider) Elements(context.Context, string) ([]computerdomain.UIElement, error) {
	return f.elements, f.elementsErr
}

func (f *fakeAccessibilityProvider) Press(_ context.Context, _ string, label string) error {
	f.pressed = label
	return f.pressErr
}

func TestExecutorAccessibilityObservationUsesFrameCoordinates(t *testing.T) {
	provider := &fakeAccessibilityProvider{elements: []computerdomain.UIElement{{
		Role: "button", Label: "Save", State: "enabled actions=press", BBox: [4]int{200, 100, 400, 200},
	}}}
	executor := newExecutor(config.DefaultConfig(), provider)
	observation := &computerdomain.Observation{Width: 1000, Height: 500}

	executor.observeAccessibility(context.Background(), "", observation, 2000, 1000)

	if len(observation.Elements) != 1 {
		t.Fatalf("Elements = %+v, want one element", observation.Elements)
	}
	if got, want := observation.Elements[0].BBox, [4]int{100, 50, 200, 100}; got != want {
		t.Fatalf("BBox = %v, want %v", got, want)
	}
	if observation.Image != nil {
		t.Fatal("accessibility observation unexpectedly captured an image")
	}
}

func TestExecutorAccessibilityDegradesToScreenshotGuidance(t *testing.T) {
	provider := &fakeAccessibilityProvider{elementsErr: accessibility.ErrPermission}
	executor := newExecutor(config.DefaultConfig(), provider)
	observation := &computerdomain.Observation{Width: 1000, Height: 500}

	executor.observeAccessibility(context.Background(), "frontmost", observation, 2000, 1000)

	if !strings.Contains(observation.Message, "screenshot action as fallback") {
		t.Fatalf("Message = %q, want screenshot fallback guidance", observation.Message)
	}
	if observation.Image != nil {
		t.Fatal("failed accessibility observation unexpectedly captured an image")
	}
}

func TestExecutorPressAccessibilityWithoutScreenshot(t *testing.T) {
	provider := &fakeAccessibilityProvider{}
	executor := newExecutor(config.DefaultConfig(), provider)
	observation := &computerdomain.Observation{}

	executor.pressAccessibility(context.Background(), "frontmost", "Save", observation)

	if provider.pressed != "Save" {
		t.Fatalf("pressed = %q, want Save", provider.pressed)
	}
	if observation.Image != nil {
		t.Fatal("accessibility press unexpectedly captured an image")
	}
}

func TestAccessibilityFallbackClassifiesErrors(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{accessibility.ErrUnsupported, "not implemented"},
		{accessibility.ErrPermission, "permission"},
		{accessibility.ErrElementNotFound, "no pressable element"},
		{errors.New("boom"), "boom"},
	}
	for _, tt := range tests {
		if got := accessibilityFallback("Unavailable", tt.err); !strings.Contains(got, tt.want) {
			t.Errorf("accessibilityFallback(%v) = %q, want substring %q", tt.err, got, tt.want)
		}
	}
}
