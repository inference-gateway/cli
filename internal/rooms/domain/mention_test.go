package domain

import (
	"testing"

	require "github.com/stretchr/testify/require"
)

func TestMentions(t *testing.T) {
	known := []string{"tester", "writer", "general-purpose"}
	tests := []struct {
		name string
		text string
		want []string
	}{
		{"one mention", "@tester run the suite", []string{"tester"}},
		{"mentions keep their order", "@writer then @tester", []string{"writer", "tester"}},
		{"a repeated mention counts once", "@tester and again @tester", []string{"tester"}},
		{"trailing punctuation is not part of the name", "thanks @tester, and @writer.", []string{"tester", "writer"}},
		{"a dashed agent name", "ask @general-purpose", []string{"general-purpose"}},
		{"a file reference is no mention", "read @README.md", nil},
		{"a dot-slash path names the file, not the agent", "see @./tester", nil},
		{"an email address is no mention", "mail tester@example.com", nil},
		{"an @token mid-word is no mention", "a@tester", nil},
		{"an unknown name is no mention", "@reviewer look", nil},
		{"no @ at all", "plain text", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, Mentions(tt.text, known))
		})
	}
}

func TestPlainMentions(t *testing.T) {
	known := []string{"tester", "general-purpose"}
	tests := []struct {
		name string
		text string
		want string
	}{
		{"a mention loses its @", "@tester run the suite", "tester run the suite"},
		{"punctuation after a mention stays", "thanks @tester, done", "thanks tester, done"},
		{"a dashed agent name", "ask @general-purpose", "ask general-purpose"},
		{"a file reference keeps its @", "read @README.md and @./tester", "read @README.md and @./tester"},
		{"an unknown name keeps its @", "@reviewer look", "@reviewer look"},
		{"an email address is left alone", "mail tester@example.com", "mail tester@example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, PlainMentions(tt.text, known))
		})
	}
}
