//go:build darwin

package macos

import (
	"runtime"
	"testing"
	"time"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"
)

func TestOverlayWindow_Creation(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only test")
	}

	overlay, err := NewOverlayWindow()
	require.NoError(t, err)
	require.NotNil(t, overlay)
	require.False(t, overlay.visible)
	defer func() {
		if overlay != nil {
			_ = overlay.Destroy()
		}
	}()
}

func TestOverlayWindow_ShowHide(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only test")
	}

	overlay, err := NewOverlayWindow()
	require.NoError(t, err)
	require.NotNil(t, overlay)
	defer func() { _ = overlay.Destroy() }()

	err = overlay.Show()
	require.NoError(t, err)
	assert.True(t, overlay.visible)

	time.Sleep(100 * time.Millisecond)

	assert.True(t, overlay.IsVisible())

	err = overlay.Hide()
	require.NoError(t, err)
	assert.False(t, overlay.visible)

	time.Sleep(100 * time.Millisecond)

	assert.False(t, overlay.IsVisible())
}

func TestOverlayWindow_Lifecycle(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only test")
	}

	overlay, err := NewOverlayWindow()
	require.NoError(t, err)
	require.NotNil(t, overlay)

	err = overlay.Show()
	require.NoError(t, err)
	assert.True(t, overlay.visible)

	time.Sleep(100 * time.Millisecond)

	err = overlay.Hide()
	require.NoError(t, err)
	assert.False(t, overlay.visible)

	err = overlay.Destroy()
	require.NoError(t, err)
	assert.False(t, overlay.visible)
}

func TestOverlayWindow_DestroyWithoutShow(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only test")
	}

	overlay, err := NewOverlayWindow()
	require.NoError(t, err)
	require.NotNil(t, overlay)

	err = overlay.Destroy()
	require.NoError(t, err)
}

func TestOverlayWindow_OperationsOnEmptyWindow(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only test")
	}

	overlay := &OverlayWindow{cmd: nil, visible: false}

	err := overlay.Hide()
	assert.NoError(t, err)

	err = overlay.Destroy()
	assert.NoError(t, err)

	assert.False(t, overlay.IsVisible())
}

func TestOverlayWindow_MultipleShowCalls(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only test")
	}

	overlay, err := NewOverlayWindow()
	require.NoError(t, err)
	require.NotNil(t, overlay)
	defer func() { _ = overlay.Destroy() }()

	err = overlay.Show()
	require.NoError(t, err)

	err = overlay.Show()
	require.NoError(t, err)

	assert.True(t, overlay.visible)
}

func TestOverlayWindow_MultipleHideCalls(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only test")
	}

	overlay, err := NewOverlayWindow()
	require.NoError(t, err)
	require.NotNil(t, overlay)
	defer func() { _ = overlay.Destroy() }()

	err = overlay.Show()
	require.NoError(t, err)

	err = overlay.Hide()
	require.NoError(t, err)

	err = overlay.Hide()
	require.NoError(t, err)

	assert.False(t, overlay.visible)
}
