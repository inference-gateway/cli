package gateway

import (
	"context"
	"fmt"
	"strings"

	sdk "github.com/inference-gateway/sdk"

	decisiondomain "github.com/inference-gateway/cli/internal/decision/domain"
)

// Answer field names of the Decisions API response.
const (
	fieldName        = "name"
	fieldType        = "type"
	fieldProbability = "probability"
	fieldChoice      = "choice"
)

// Decider answers questions through the gateway's Decisions API, so the
// provider behind a "provider/model" id is the gateway's concern.
type Decider struct {
	client sdk.Client
}

// New returns a Decider calling the Decisions API on client.
func New(client sdk.Client) *Decider {
	return &Decider{client: client}
}

// Decide sends evidence and questions as one decision request and maps the
// typed answers back by name.
func (d *Decider) Decide(ctx context.Context, model string, evidence string, questions []decisiondomain.Question) (decisiondomain.Decisions, error) {
	provider, modelName, found := strings.Cut(model, "/")
	if !found || provider == "" || modelName == "" {
		return decisiondomain.Decisions{}, fmt.Errorf("invalid decision model %q, expected 'provider/model'", model)
	}
	request, err := buildRequest(modelName, evidence, questions)
	if err != nil {
		return decisiondomain.Decisions{}, err
	}
	decision, err := d.client.CreateDecision(ctx, sdk.Provider(provider), request)
	if err != nil {
		return decisiondomain.Decisions{}, fmt.Errorf("decision call failed: %w", err)
	}
	return parseDecision(decision), nil
}

func buildRequest(model string, evidence string, questions []decisiondomain.Question) (sdk.CreateDecisionRequest, error) {
	request := sdk.CreateDecisionRequest{Model: model, Questions: make([]sdk.DecisionQuestion, 0, len(questions))}
	if err := request.Input.FromCreateDecisionRequestInput0(evidence); err != nil {
		return sdk.CreateDecisionRequest{}, fmt.Errorf("encoding decision evidence: %w", err)
	}
	for _, question := range questions {
		request.Questions = append(request.Questions, buildQuestion(question))
	}
	return request, nil
}

func buildQuestion(question decisiondomain.Question) sdk.DecisionQuestion {
	out := sdk.DecisionQuestion{
		Name:         question.Name,
		Type:         sdk.DecisionQuestionType(question.Type),
		Instructions: question.Instructions,
	}
	if len(question.Choices) > 0 {
		choices := make([]struct {
			Description *string `json:"description,omitempty"`
			Value       *string `json:"value,omitempty"`
		}, len(question.Choices))
		for i, choice := range question.Choices {
			choices[i].Value, choices[i].Description = ptr(choice.Value), ptr(choice.Description)
		}
		out.Choices = &choices
	}
	return out
}

func parseDecision(decision *sdk.Decision) decisiondomain.Decisions {
	var out decisiondomain.Decisions
	if decision == nil {
		return out
	}
	if decision.Usage != nil && decision.Usage.InputTokens != nil {
		out.InputTokens = *decision.Usage.InputTokens
	}
	if decision.Answers == nil {
		return out
	}
	out.Answers = make([]decisiondomain.Answer, 0, len(*decision.Answers))
	for _, raw := range *decision.Answers {
		out.Answers = append(out.Answers, parseAnswer(raw))
	}
	return out
}

func parseAnswer(raw map[string]any) decisiondomain.Answer {
	return decisiondomain.Answer{
		Name:        text(raw[fieldName]),
		Type:        decisiondomain.QuestionType(text(raw[fieldType])),
		Probability: number(raw[fieldProbability]),
		Choice:      text(raw[fieldChoice]),
	}
}

func text(v any) string {
	s, _ := v.(string)
	return s
}

func number(v any) float64 {
	f, _ := v.(float64)
	return f
}

func ptr(s string) *string {
	return &s
}
