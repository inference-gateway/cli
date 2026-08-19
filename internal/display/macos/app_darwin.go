//go:build darwin

package macos

/*
#include <stdlib.h>
*/
import "C"

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unsafe"

	display "github.com/inference-gateway/cli/internal/display"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// macosAppProvider implements display.AppProvider using macOS's NSWorkspace.
// It does NOT require accessibility permissions (those are only needed for
// UI element inspection via AX APIs; NSWorkspace enumeration and activation
// work without them).
type macosAppProvider struct{}

var _ display.AppProvider = (*macosAppProvider)(nil)

func (p *macosAppProvider) ListRunning(ctx context.Context) ([]domain.Application, error) {
	cStr := C.listRunningApps()
	defer C.free(unsafe.Pointer(cStr))
	s := C.GoString(cStr)

	var apps []domain.Application
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			continue
		}
		id := parts[0]
		name := parts[1]
		pidStr := parts[2]

		if id == "" {
			id = "pid:" + pidStr
		}

		apps = append(apps, domain.Application{
			ID:         id,
			Name:       name,
			PlatformID: pidStr,
		})
	}

	return apps, nil
}

func (p *macosAppProvider) Activate(ctx context.Context, id string) error {
	// Try as bundle ID first
	cStr := C.CString(id)
	defer C.free(unsafe.Pointer(cStr))

	if C.activateApp(cStr) {
		return nil
	}

	// Try as "pid:N" format
	if strings.HasPrefix(id, "pid:") {
		pidStr := strings.TrimPrefix(id, "pid:")
		pid, err := strconv.Atoi(pidStr)
		if err == nil && C.activateAppByPID(C.int(pid)) {
			return nil
		}
	}

	return fmt.Errorf("failed to activate app %q: %w", id, display.ErrAppNotFound)
}

func (p *macosAppProvider) GetFocused(ctx context.Context) (*domain.Application, error) {
	cPID := C.getFrontmostPID()
	pid := int(cPID)
	if pid <= 0 {
		return nil, nil
	}

	cName := C.getFrontmostName()
	defer C.free(unsafe.Pointer(cName))
	name := C.GoString(cName)

	cBundleID := C.getFrontmostApp()
	defer C.free(unsafe.Pointer(cBundleID))
	bundleID := C.GoString(cBundleID)

	id := bundleID
	if id == "" {
		id = fmt.Sprintf("pid:%d", pid)
	}

	return &domain.Application{
		ID:         id,
		Name:       name,
		PlatformID: strconv.Itoa(pid),
	}, nil
}

func (p *macosAppProvider) Close() error {
	return nil
}

func init() {
	display.RegisterAppProvider(&macosAppProvider{})
	logger.Debug("registered macOS app provider")
}
