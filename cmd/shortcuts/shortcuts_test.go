package shortcuts

import (
	"bytes"
	"encoding/json"
	"testing"

	require "github.com/stretchr/testify/require"

	cobra "github.com/spf13/cobra"

	config "github.com/inference-gateway/cli/config"
)

func TestListJSON(t *testing.T) {
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, list(cmd, config.DefaultConfig(), "json"))

	var parsed struct {
		Shortcuts []Entry `json:"shortcuts"`
		Total     int     `json:"total"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &parsed))
	require.Equal(t, len(parsed.Shortcuts), parsed.Total)

	names := map[string]Entry{}
	for _, e := range parsed.Shortcuts {
		names[e.Name] = e
	}
	for _, want := range []string{"init", "install-opentask", "help"} {
		e, ok := names[want]
		require.True(t, ok, "expected %q in %v", want, parsed.Shortcuts)
		require.NotEmpty(t, e.Description)
		require.NotEmpty(t, e.Usage)
	}
}

func TestListText(t *testing.T) {
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, list(cmd, nil, "text"))
	require.Contains(t, out.String(), "/init - ")
}
