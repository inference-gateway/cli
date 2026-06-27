package domain

// UserQuestionOption is a single selectable choice within a UserQuestion.
type UserQuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// UserQuestion is one clarifying question the agent asks the user via the
// AskUserQuestion tool. It mirrors the tool schema: a short header chip, the
// question text, 2-4 options, and whether multiple options may be selected.
type UserQuestion struct {
	Header      string               `json:"header"`
	Question    string               `json:"question"`
	Options     []UserQuestionOption `json:"options"`
	MultiSelect bool                 `json:"multiSelect"`
}

// UserQuestionAnswer is the user's response to one UserQuestion. SelectedLabels
// holds the chosen option label(s); OtherText is non-empty when the user picked
// the synthesized "Other" free-text choice (it may coexist with selected
// labels in multi-select). Header and Question are echoed so the tool result is
// self-describing for the model.
type UserQuestionAnswer struct {
	Header         string   `json:"header"`
	Question       string   `json:"question"`
	SelectedLabels []string `json:"selectedLabels"`
	OtherText      string   `json:"otherText,omitempty"`
}
