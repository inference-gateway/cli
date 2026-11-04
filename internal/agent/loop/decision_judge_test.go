package loop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	decisionmocks "github.com/inference-gateway/cli/tests/mocks/decision"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	decisiondomain "github.com/inference-gateway/cli/internal/decision/domain"
)

const (
	decisionTestModel  = "ollama_cloud/clef:27b"
	decisionTestTokens = 42
)

func decisionTestConfig(onError string, minConfidence float64) *config.Config {
	cfg := config.DefaultConfig()
	cfg.Agent.Model = decisionTestModel
	cfg.Judge = config.JudgeConfig{Backend: config.JudgeBackendDecisions, OnError: onError, MinConfidence: minConfidence}
	return cfg
}

func answers(risk string, probabilities map[string]float64) decisiondomain.Decisions {
	out := decisiondomain.Decisions{InputTokens: decisionTestTokens}
	for id, p := range probabilities {
		out.Answers = append(out.Answers, decisiondomain.Answer{Name: id, Type: decisiondomain.Predicate, Probability: p})
	}
	if risk != "" {
		out.Answers = append(out.Answers, decisiondomain.Answer{Name: riskQuestionName, Type: decisiondomain.Choice, Choice: risk})
	}
	return out
}

func decisionInput(ids ...string) agentdomain.JudgeInput {
	in := agentdomain.JudgeInput{Model: decisionTestModel, RootIntent: "set up the project", Intent: "install the dependency"}
	for _, id := range ids {
		in.Actions = append(in.Actions, agentdomain.JudgeAction{ID: id, Action: `Bash: {"command": "go get"}`})
	}
	return in
}

func TestDecisionJudge_ThresholdsProbability(t *testing.T) {
	tests := []struct {
		name        string
		probability float64
		approved    bool
		reason      string
	}{
		{"above the threshold approves", 0.93, true, "decisions: p_approve=0.93, risk: benign"},
		{"at the threshold approves", 0.7, true, "p_approve=0.70"},
		{"below the threshold rejects", 0.31, false, "rejected by the judge (decisions: p_approve=0.31, risk: benign, below min_confidence 0.70)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decider := &decisionmocks.FakeDecider{}
			decider.DecideReturns(answers("benign", map[string]float64{"call-0": tt.probability}), nil)
			judge := NewDecisionJudge(decider, decisionTestConfig("", 0))

			out, err := judge.Judge(context.Background(), decisionInput("call-0"))
			if err != nil {
				t.Fatalf("Judge() error = %v", err)
			}
			if got := out.Verdicts[0].Approved(); got != tt.approved {
				t.Fatalf("approved = %v, want %v (reason %q)", got, tt.approved, out.Verdicts[0].Reason)
			}
			if !strings.Contains(out.Verdicts[0].Reason, tt.reason) {
				t.Fatalf("reason = %q, want it to contain %q", out.Verdicts[0].Reason, tt.reason)
			}
			if out.Usage == nil || out.Usage.PromptTokens != decisionTestTokens {
				t.Fatalf("usage = %+v, want %d prompt tokens", out.Usage, decisionTestTokens)
			}
		})
	}
}

func TestDecisionJudge_AsksOnePredicatePerCallPlusRisk(t *testing.T) {
	decider := &decisionmocks.FakeDecider{}
	decider.DecideReturns(answers("benign", map[string]float64{"call-0": 0.9, "call-1": 0.9}), nil)
	judge := NewDecisionJudge(decider, decisionTestConfig("", 0))

	if _, err := judge.Judge(context.Background(), decisionInput("call-0", "call-1")); err != nil {
		t.Fatalf("Judge() error = %v", err)
	}
	if decider.DecideCallCount() != 1 {
		t.Fatalf("decide calls = %d, want 1", decider.DecideCallCount())
	}
	_, model, evidence, questions := decider.DecideArgsForCall(0)
	if model != decisionTestModel {
		t.Fatalf("model = %q", model)
	}
	for _, want := range []string{"<root_request>\nset up the project", "<latest_request>\ninstall the dependency", `<tool_call id="call-1">`} {
		if !strings.Contains(evidence, want) {
			t.Fatalf("evidence lacks %q:\n%s", want, evidence)
		}
	}
	if len(questions) != 3 {
		t.Fatalf("questions = %d, want 2 predicates + risk", len(questions))
	}
	if questions[0].Name != "call-0" || questions[0].Type != decisiondomain.Predicate || !strings.Contains(questions[0].Instructions, `"call-0"`) {
		t.Fatalf("first question = %+v", questions[0])
	}
	if questions[2].Name != riskQuestionName || questions[2].Type != decisiondomain.Choice || len(questions[2].Choices) != len(riskClasses) {
		t.Fatalf("risk question = %+v", questions[2])
	}
}

func TestDecisionJudge_MissingAnswerFailsClosedAlone(t *testing.T) {
	decider := &decisionmocks.FakeDecider{}
	decider.DecideReturns(answers("", map[string]float64{"call-0": 0.9}), nil)
	judge := NewDecisionJudge(decider, decisionTestConfig("", 0))

	out, err := judge.Judge(context.Background(), decisionInput("call-0", "call-1"))
	if err != nil {
		t.Fatalf("Judge() error = %v", err)
	}
	if !out.Verdicts[0].Approved() || out.Verdicts[1].Approved() {
		t.Fatalf("verdicts = %+v", out.Verdicts)
	}
	if !strings.Contains(out.Verdicts[1].Reason, "judge unavailable: judge gave no answer for call call-1") {
		t.Fatalf("reason = %q", out.Verdicts[1].Reason)
	}
}

func TestDecisionJudge_OnError(t *testing.T) {
	tests := []struct {
		onError  string
		approved bool
	}{
		{config.JudgeOnErrorDeny, false},
		{config.JudgeOnErrorAllow, true},
	}
	for _, tt := range tests {
		t.Run(tt.onError, func(t *testing.T) {
			decider := &decisionmocks.FakeDecider{}
			decider.DecideReturns(decisiondomain.Decisions{}, errors.New("gateway down"))
			judge := NewDecisionJudge(decider, decisionTestConfig(tt.onError, 0))

			out, err := judge.Judge(context.Background(), decisionInput("call-0"))
			if err != nil {
				t.Fatalf("Judge() error = %v", err)
			}
			if out.Verdicts[0].Approved() != tt.approved || !strings.Contains(out.Verdicts[0].Reason, "judge unavailable: gateway down") {
				t.Fatalf("verdict = %+v", out.Verdicts[0])
			}
		})
	}
}

func TestDecisionJudge_SplitsOversizedBatch(t *testing.T) {
	decider := &decisionmocks.FakeDecider{}
	decider.DecideStub = func(_ context.Context, _ string, _ string, questions []decisiondomain.Question) (decisiondomain.Decisions, error) {
		probabilities := make(map[string]float64, len(questions))
		for _, q := range questions[:len(questions)-1] {
			probabilities[q.Name] = 0.9
		}
		return answers("benign", probabilities), nil
	}
	judge := NewDecisionJudge(decider, decisionTestConfig("", 0))

	ids := make([]string, maxJudgeBatchSize+1)
	for i := range ids {
		ids[i] = fmt.Sprintf("call-%d", i)
	}
	out, err := judge.Judge(context.Background(), decisionInput(ids...))
	if err != nil {
		t.Fatalf("Judge() error = %v", err)
	}
	if decider.DecideCallCount() != 2 || len(out.Verdicts) != len(ids) {
		t.Fatalf("calls = %d, verdicts = %d", decider.DecideCallCount(), len(out.Verdicts))
	}
	if out.Usage.PromptTokens != 2*decisionTestTokens {
		t.Fatalf("usage = %+v", out.Usage)
	}
}

func TestDecisionJudge_InvalidModel(t *testing.T) {
	judge := NewDecisionJudge(&decisionmocks.FakeDecider{}, decisionTestConfig("", 0))
	in := decisionInput("call-0")
	in.Model = "no-provider"
	if _, err := judge.Judge(context.Background(), in); err == nil {
		t.Fatal("expected an invalid model error")
	}
}

func TestNewJudge_PicksBackend(t *testing.T) {
	if _, ok := newJudge(nil, decisionTestConfig("", 0)).(*DecisionJudge); !ok {
		t.Fatal("backend decisions must build a DecisionJudge")
	}
	if _, ok := newJudge(nil, judgeTestConfig("")).(*LLMJudge); !ok {
		t.Fatal("the default backend must build an LLMJudge")
	}
}
