//go:build !darwin

package accessibility

import (
	"fmt"
	"runtime"
)

func nativeResponse(request) response {
	return errorResponse(fmt.Errorf("%w: %s", ErrUnsupported, runtime.GOOS))
}
