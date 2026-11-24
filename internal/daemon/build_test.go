package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	require "github.com/stretchr/testify/require"
)

func TestBuildNoticeTellsAnotherBuildApart(t *testing.T) {
	require.NotEmpty(t, Build(), "the test binary has a build")
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, os.MkdirAll(filepath.Dir(PIDFile()), 0o700))
	require.NoError(t, os.WriteFile(PIDFile(), []byte("4242\n"), 0o600))

	tests := []struct {
		name        string
		daemonBuild string
		wantNotice  bool
	}{
		{"the same build", Build(), false},
		{"another build", "0123abcd", true},
		{"a daemon declaring no build", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notice := BuildNotice(tt.daemonBuild)
			if !tt.wantNotice {
				require.Empty(t, notice)
				return
			}
			require.Contains(t, notice, "another build")
			require.Contains(t, notice, "(pid 4242)")
		})
	}
}

func TestBuildNoticeWithoutAPidFileNamesNoPid(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	notice := BuildNotice("0123abcd")
	require.Contains(t, notice, "another build")
	require.False(t, strings.Contains(notice, "pid"), "notice = %q", notice)
}
