package domain

import "errors"

// ErrPromptNotFound is returned when a conversation holds no prompt a
// PromptRef names, so a rewind leaves it as it was.
var ErrPromptNotFound = errors.New("the conversation has no such prompt")

// PromptRef names one of the user's prompts the way a client sees it: by its
// text, and by which of the times the user sent that text it is, counted from
// the newest. An Occurrence of 1, or less, is the last time and 2 the time
// before. Entries carry no id a client could hold on to instead.
type PromptRef struct {
	Text       string
	Occurrence int
}
