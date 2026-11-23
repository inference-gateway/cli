package domain

import (
	"regexp"
	"slices"
	"strings"
)

// mentionPattern matches an @token that starts the text or follows whitespace,
// so an email address or an @ inside a word is never a mention.
var mentionPattern = regexp.MustCompile(`(?:^|\s)@(\S+)`)

// mentionTrailing is the punctuation a sentence puts after a mention, which is
// not part of the agent's name.
const mentionTrailing = ".,:;!?)]}'\""

// Mentions returns the agents text @mentions, in order and without repeats. An
// @token is a mention only when it names one of known, so a member's name wins
// over a file of the same name, while @README.md or @./tester stay file
// references for the member's file expansion.
func Mentions(text string, known []string) []string {
	var mentions []string
	for _, match := range mentionPattern.FindAllStringSubmatch(text, -1) {
		name := strings.TrimRight(match[1], mentionTrailing)
		if slices.Contains(known, name) && !slices.Contains(mentions, name) {
			mentions = append(mentions, name)
		}
	}
	return mentions
}
