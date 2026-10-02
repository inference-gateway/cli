package shortcuts

import (
	"testing"

	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
)

// TestNewMetadataRegistryRegistersEffortAndVoice pins name agreement between the
// metadata registry and the chat TUI's registry: a custom shortcut named effort or
// voice must be listed as built-in-shadowed on every surface, not silently
// loadable in `infer shortcuts list` and the channel menu while the TUI ignores it.
func TestNewMetadataRegistryRegistersEffortAndVoice(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := config.DefaultConfig()
	reg := NewMetadataRegistry(cfg)
	require.Contains(t, reg.List(), "effort")
	require.Contains(t, reg.List(), "reload")
	require.NotContains(t, reg.List(), "voice", "voice registers only when speech-to-text is enabled")

	cfg.SpeechToText.Enabled = true
	reg = NewMetadataRegistry(cfg)
	require.Contains(t, reg.List(), "effort")
	require.Contains(t, reg.List(), "voice")
}
