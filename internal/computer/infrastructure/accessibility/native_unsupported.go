//go:build !darwin

package accessibility

import (
	"fmt"
	"runtime"

	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
)

func runNative(request) ([]computerdomain.UIElement, error) {
	return nil, fmt.Errorf("%w: %s", ErrUnsupported, runtime.GOOS)
}
