package github

import (
	"testing"

	require "github.com/stretchr/testify/require"
)

func TestToken(t *testing.T) {
	tests := []struct {
		name        string
		githubToken string
		ghToken     string
		want        string
	}{
		{name: "prefers GITHUB_TOKEN over GH_TOKEN", githubToken: "primary", ghToken: "secondary", want: "primary"},
		{name: "falls back to GH_TOKEN", githubToken: "", ghToken: "secondary", want: "secondary"},
		{name: "trims surrounding whitespace", githubToken: "  spaced  ", ghToken: "", want: "spaced"},
		{name: "empty when neither set", githubToken: "", ghToken: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GITHUB_TOKEN", tt.githubToken)
			t.Setenv("GH_TOKEN", tt.ghToken)
			require.Equal(t, tt.want, Token())
		})
	}
}
