package chat

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"

	daemon "github.com/inference-gateway/cli/internal/daemon"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

// TestAttachErrorNamesTheDaemonThatRefusedTheToken pins the message of a chat
// a running daemon refused: it says the daemon runs with another token and
// names its pid to stop, while any other failure points at the daemon's log.
func TestAttachErrorNamesTheDaemonThatRefusedTheToken(t *testing.T) {
	refused := fmt.Errorf("connecting: %w", &agui.HelloRefusedError{Addr: "127.0.0.1:52789", Reason: agui.RefusalNotAuthenticated})
	tests := []struct {
		name string
		err  error
		pid  string
		want string
	}{
		{
			"a refused token names the daemon to stop", refused, "33154",
			"could not reach infer daemon: connecting: the binding on 127.0.0.1:52789 refused the hello: not authenticated - it runs with " +
				"another token than this chat, such as one from before infer init --overwrite, so stop it (pid 33154) and run again",
		},
		{
			"without a pid file it still says why", refused, "",
			"could not reach infer daemon: connecting: the binding on 127.0.0.1:52789 refused the hello: not authenticated - it runs with " +
				"another token than this chat, such as one from before infer init --overwrite, so stop it and run again",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			if tt.pid != "" {
				require.NoError(t, os.MkdirAll(filepath.Dir(daemon.PIDFile()), 0o755))
				require.NoError(t, os.WriteFile(daemon.PIDFile(), []byte(tt.pid+"\n"), 0o644))
			}
			require.EqualError(t, attachError(tt.err), tt.want)
		})
	}

	t.Run("any other failure points at the log", func(t *testing.T) {
		require.ErrorContains(t, attachError(errors.New("dial tcp: connection refused")), "daemon-<date>.log is in")
	})
}
