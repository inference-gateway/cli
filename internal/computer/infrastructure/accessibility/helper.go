package accessibility

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// IsHelperProcess reports whether this process was launched as the isolated
// accessibility bridge. The CLI entry point checks this before constructing
// any services or presentation state.
func IsHelperProcess() bool {
	return os.Getenv(helperEnvironment) == "1"
}

// RunHelper serves one JSON request and writes one JSON response.
func RunHelper(input io.Reader, output io.Writer) (err error) {
	encoder := json.NewEncoder(output)
	defer func() {
		if recovered := recover(); recovered != nil {
			err = encoder.Encode(response{Code: "unavailable", Error: fmt.Sprintf("native bridge panic: %v", recovered)})
		}
	}()

	var req request
	if err := json.NewDecoder(input).Decode(&req); err != nil {
		return encoder.Encode(response{Code: "unavailable", Error: "decode request: " + err.Error()})
	}
	elements, nativeErr := runNative(req)
	if nativeErr != nil {
		return encoder.Encode(errorResponse(nativeErr))
	}
	return encoder.Encode(response{Elements: elements})
}

func errorResponse(err error) response {
	code := "unavailable"
	switch {
	case errors.Is(err, ErrUnsupported):
		code = "unsupported"
	case errors.Is(err, ErrPermission):
		code = "permission"
	case errors.Is(err, ErrElementNotFound):
		code = "not_found"
	}
	return response{Code: code, Error: err.Error()}
}
