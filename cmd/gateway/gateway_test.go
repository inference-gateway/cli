package gatewaycmd_test

import (
	"bytes"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	root "github.com/inference-gateway/cli/cmd/root"
)

func TestInstallRejectsAVersionThatIsNoRelease(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  string
	}{
		{"from the flag", []string{"gateway", "install", "--version", "main"}, ""},
		{"from the environment", []string{"gateway", "install"}, "main"},
		{"a flag that is no release wins over a valid environment", []string{"gateway", "install", "--version", "0.65"}, "0.65.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Chdir(home)
			if tt.env != "" {
				t.Setenv("INFER_GATEWAY_VERSION", tt.env)
			}

			output := new(bytes.Buffer)
			command := root.NewCommand()
			command.SetOut(output)
			command.SetErr(output)
			command.SetArgs(tt.args)

			err := command.ExecuteContext(t.Context())

			require.Error(t, err)
			assert.Contains(t, err.Error(), "gateway.version")
			assert.NoDirExists(t, home+"/.infer/bin", "nothing is downloaded for a version that is no release")
		})
	}
}

func TestInstallTakesNoArguments(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Chdir(home)

	command := root.NewCommand()
	command.SetOut(new(bytes.Buffer))
	command.SetErr(new(bytes.Buffer))
	command.SetArgs([]string{"gateway", "install", "0.65.0"})

	require.Error(t, command.ExecuteContext(t.Context()))
	assert.NoDirExists(t, home+"/.infer/bin")
}
