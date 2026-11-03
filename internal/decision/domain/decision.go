package domain

import "context"

// QuestionType is the kind of answer a question asks for.
type QuestionType string

// Question kinds: a predicate answers with one probability, a choice picks
// one option with a probability per option, a score places the evidence on an
// ordered scale.
const (
	Predicate QuestionType = "predicate"
	Choice    QuestionType = "choice"
	Score     QuestionType = "score"
)

// Option is one answer of a choice question.
type Option struct {
	Value       string
	Description string
}

// Level is one step of a score question's scale, lowest first.
type Level struct {
	Label       string
	Description string
}

// Question is one typed question asked about shared evidence. Name identifies
// its answer. Choices apply to Choice questions and Levels to Score questions.
type Question struct {
	Name         string
	Type         QuestionType
	Instructions string
	Choices      []Option
	Levels       []Level
}

// Answer is the decision model's answer to one question. Probability holds a
// predicate's p(true). Choice and Probabilities hold a choice's pick and its
// distribution. Score holds the expected level of a score question.
// Confidence is the model's own certainty, 0 when the backend reports none.
type Answer struct {
	Name          string
	Type          QuestionType
	Probability   float64
	Choice        string
	Probabilities map[string]float64
	Score         float64
	Confidence    float64
}

// Decisions is the answer set of one request plus the input tokens it cost.
type Decisions struct {
	Answers     []Answer
	InputTokens int
}

// ByName returns the answer named name and whether it was given.
func (d Decisions) ByName(name string) (Answer, bool) {
	for _, answer := range d.Answers {
		if answer.Name == name {
			return answer, true
		}
	}
	return Answer{}, false
}

// Decider answers typed questions about shared evidence with native
// probabilities. model is the "provider/model" id of the decision model.
type Decider interface {
	Decide(ctx context.Context, model string, evidence string, questions []Question) (Decisions, error)
}
