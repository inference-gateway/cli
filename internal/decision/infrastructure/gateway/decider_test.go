package gateway

import (
	"context"
	"errors"
	"strings"
	"testing"

	sdkmocks "github.com/inference-gateway/cli/tests/mocks/sdk"

	sdk "github.com/inference-gateway/sdk"

	decisiondomain "github.com/inference-gateway/cli/internal/decision/domain"
)

const (
	testModel    = "ollama_cloud/clef:27b"
	testEvidence = "<root_request>\nship it\n</root_request>"
	testTokens   = 12
)

func decisionResponse() *sdk.Decision {
	answers := []map[string]any{
		{"name": "call-0", "type": "predicate", "probability": 0.93},
		{"name": "risk", "type": "choice", "choice": "benign", "confidence": 0.8, "probabilities": map[string]any{"benign": 0.9, "destructive": 0.1}},
		{"name": "tone", "type": "score", "score": 1.5},
	}
	tokens := testTokens
	decision := &sdk.Decision{Answers: &answers}
	decision.Usage = &struct {
		InputTokens        *int `json:"input_tokens,omitempty"`
		InputTokensDetails *struct {
			CacheWriteTokens *int `json:"cache_write_tokens,omitempty"`
			CachedTokens     *int `json:"cached_tokens,omitempty"`
		} `json:"input_tokens_details,omitempty"`
	}{InputTokens: &tokens}
	return decision
}

func TestDecider_BuildsRequestAndParsesAnswers(t *testing.T) {
	client := &sdkmocks.FakeClient{}
	client.CreateDecisionReturns(decisionResponse(), nil)
	questions := []decisiondomain.Question{
		{Name: "call-0", Type: decisiondomain.Predicate, Instructions: "safe?"},
		{Name: "risk", Type: decisiondomain.Choice, Instructions: "class?", Choices: []decisiondomain.Option{{Value: "benign", Description: "fine"}}},
		{Name: "tone", Type: decisiondomain.Score, Instructions: "rate", Levels: []decisiondomain.Level{{Label: "rude"}, {Label: "kind", Description: "warm"}}},
	}

	out, err := New(client).Decide(context.Background(), testModel, testEvidence, questions)
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}

	_, provider, request := client.CreateDecisionArgsForCall(0)
	if provider != "ollama_cloud" || request.Model != "clef:27b" {
		t.Fatalf("provider/model = %s/%s", provider, request.Model)
	}
	if input, _ := request.Input.AsCreateDecisionRequestInput0(); input != testEvidence {
		t.Fatalf("input = %q", input)
	}
	if len(request.Questions) != 3 || request.Questions[0].Type != sdk.Predicate || request.Questions[0].Name != "call-0" {
		t.Fatalf("questions = %+v", request.Questions)
	}
	if choices := *request.Questions[1].Choices; *choices[0].Value != "benign" || *choices[0].Description != "fine" {
		t.Fatalf("choices = %+v", choices)
	}
	if levels := *request.Questions[2].Levels; *levels[1].Label != "kind" || *levels[1].Description != "warm" {
		t.Fatalf("levels = %+v", levels)
	}

	if out.InputTokens != testTokens || len(out.Answers) != 3 {
		t.Fatalf("decisions = %+v", out)
	}
	approve, _ := out.ByName("call-0")
	if approve.Type != decisiondomain.Predicate || approve.Probability != 0.93 {
		t.Fatalf("predicate answer = %+v", approve)
	}
	risk, _ := out.ByName("risk")
	if risk.Choice != "benign" || risk.Confidence != 0.8 || risk.Probabilities["destructive"] != 0.1 {
		t.Fatalf("choice answer = %+v", risk)
	}
	tone, _ := out.ByName("tone")
	if tone.Score != 1.5 {
		t.Fatalf("score answer = %+v", tone)
	}
}

func TestDecider_Errors(t *testing.T) {
	client := &sdkmocks.FakeClient{}
	client.CreateDecisionReturns(nil, errors.New("The Decisions API is not enabled. Set DECISIONS_ENABLED=true to enable it."))
	decider := New(client)

	if _, err := decider.Decide(context.Background(), "clef", testEvidence, nil); err == nil || !strings.Contains(err.Error(), "expected 'provider/model'") {
		t.Fatalf("model error = %v", err)
	}
	_, err := decider.Decide(context.Background(), testModel, testEvidence, nil)
	if err == nil || !strings.Contains(err.Error(), "decision call failed: The Decisions API is not enabled") {
		t.Fatalf("client error = %v", err)
	}
}

func TestDecider_EmptyDecision(t *testing.T) {
	client := &sdkmocks.FakeClient{}
	client.CreateDecisionReturns(&sdk.Decision{}, nil)

	out, err := New(client).Decide(context.Background(), testModel, testEvidence, nil)
	if err != nil || len(out.Answers) != 0 || out.InputTokens != 0 {
		t.Fatalf("out = %+v, err = %v", out, err)
	}
}
