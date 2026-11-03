package loop

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	decisiondomain "github.com/inference-gateway/cli/internal/decision/domain"
	decisiongateway "github.com/inference-gateway/cli/internal/decision/infrastructure/gateway"
	streamevent "github.com/inference-gateway/cli/internal/platform/streamevent"
)

// The decision judge asks one predicate per gated call plus one risk choice
// over the same evidence. Every option carries its own criterion because
// decision models read criteria literally.
const (
	riskQuestionName       = "risk"
	riskQuestionInstructs  = "Which risk class best describes the pending tool calls as a whole?"
	approveInstructionsFmt = "Does the tool call with id %q serve the root request and the latest request, and is it safe to run unattended without destroying data, leaking secrets or leaving the task's scope?"
)

// riskClasses are the options of the risk choice, benign first.
var riskClasses = []decisiondomain.Option{
	{Value: "benign", Description: "Reads, builds, tests or edits inside the project that the request asked for"},
	{Value: "destructive", Description: "Deletes, overwrites or force-pushes data that cannot be recovered"},
	{Value: "network", Description: "Reaches outside the machine, uploads content or installs from the network"},
	{Value: "secrets", Description: "Reads, prints or sends credentials, keys or tokens"},
	{Value: "out_of_scope", Description: "Does something the request never asked for"},
}

// DecisionJudge implements agentdomain.JudgeApprover with a decision model:
// p(approve) is thresholded against judge.min_confidence, so the gate fails
// closed on a number instead of parsed prose.
type DecisionJudge struct {
	decider decisiondomain.Decider
	config  *config.Config
}

// NewDecisionJudge creates the decisions-backed judge approver.
func NewDecisionJudge(decider decisiondomain.Decider, cfg *config.Config) *DecisionJudge {
	return &DecisionJudge{decider: decider, config: cfg}
}

// newJudge picks the judge backend of judge.yaml. Both backends share the
// judge gateway client, so judge.gateway_url applies to either.
func newJudge(client sdk.Client, cfg *config.Config) agentdomain.JudgeApprover {
	if cfg.Judge.UsesDecisions() {
		return NewDecisionJudge(decisiongateway.New(judgeClient(client, cfg)), cfg)
	}
	return NewLLMJudge(judgeCompleter(client, cfg), cfg)
}

// Judge decides the pending tool calls in batches of maxJudgeBatchSize, one
// decision request each, and returns the verdicts in input order.
func (j *DecisionJudge) Judge(ctx context.Context, in agentdomain.JudgeInput) (agentdomain.JudgeVerdicts, error) {
	if in.Model == "" {
		return agentdomain.JudgeVerdicts{}, fmt.Errorf("no judge model configured: set judge.model in %s or agent.model", config.DefaultJudgePath)
	}
	if provider, _ := config.ParseModel(in.Model); provider == "" {
		return agentdomain.JudgeVerdicts{}, fmt.Errorf("invalid judge model format %q, expected 'provider/model'", in.Model)
	}

	out := agentdomain.JudgeVerdicts{Verdicts: make([]agentdomain.JudgeVerdict, 0, len(in.Actions))}
	for batch := range slices.Chunk(in.Actions, maxJudgeBatchSize) {
		verdicts, usage := j.judgeBatch(ctx, in, batch)
		out.Verdicts = append(out.Verdicts, verdicts...)
		out.Usage = addUsage(out.Usage, usage)
	}
	return out, nil
}

// judgeBatch sends one decision request per batch, with one predicate per
// action in it. A failed request applies on_error to the whole batch, a
// missing answer to that call only.
func (j *DecisionJudge) judgeBatch(ctx context.Context, in agentdomain.JudgeInput, batch []agentdomain.JudgeAction) ([]agentdomain.JudgeVerdict, *sdk.CompletionUsage) {
	jcfg := j.config.Judge.Effective()
	decisions, err := j.decide(ctx, in, batch, jcfg)
	risk := riskLabel(decisions)
	verdicts := make([]agentdomain.JudgeVerdict, len(batch))
	for i, id := range actionIDs(batch) {
		if err != nil {
			verdicts[i] = onErrorVerdict(id, err, jcfg.OnError)
			continue
		}
		answer, ok := decisions.ByName(id)
		if !ok {
			verdicts[i] = onErrorVerdict(id, fmt.Errorf("judge gave no answer for call %s", id), jcfg.OnError)
			continue
		}
		verdicts[i] = verdictFor(id, answer.Probability, jcfg.MinConfidence, risk)
	}
	return verdicts, decisionUsage(decisions)
}

func (j *DecisionJudge) decide(ctx context.Context, in agentdomain.JudgeInput, batch []agentdomain.JudgeAction, jcfg config.JudgeConfig) (decisiondomain.Decisions, error) {
	evidence := strings.NewReplacer(
		"{root_intent}", truncateForJudge(in.RootIntent, maxJudgeIntentLength),
		"{intent}", truncateForJudge(in.Intent, maxJudgeIntentLength),
		"{action}", renderJudgeActions(batch),
	).Replace(jcfg.Prompt)
	questions := decisionQuestions(batch)
	streamevent.EmitDebugEvent("judge_request", map[string]any{
		"model": in.Model, "backend": config.JudgeBackendDecisions, "evidence": evidence, "calls": len(batch),
	})

	jctx, cancel := context.WithTimeout(ctx, time.Duration(jcfg.Timeout)*time.Second)
	defer cancel()
	return j.decider.Decide(jctx, in.Model, evidence, questions)
}

// decisionQuestions asks one predicate per call, named by the call id so the
// answer maps back without parsing, plus the shared risk choice.
func decisionQuestions(batch []agentdomain.JudgeAction) []decisiondomain.Question {
	questions := make([]decisiondomain.Question, 0, len(batch)+1)
	for _, action := range batch {
		questions = append(questions, decisiondomain.Question{
			Name:         action.ID,
			Type:         decisiondomain.Predicate,
			Instructions: fmt.Sprintf(approveInstructionsFmt, action.ID),
		})
	}
	return append(questions, decisiondomain.Question{
		Name:         riskQuestionName,
		Type:         decisiondomain.Choice,
		Instructions: riskQuestionInstructs,
		Choices:      riskClasses,
	})
}

// verdictFor thresholds p(approve): below min_confidence rejects, and the
// reason carries the probability and risk class so the agent can adjust and
// RequestApproval escalation keeps working.
func verdictFor(id string, probability, minConfidence float64, risk string) agentdomain.JudgeVerdict {
	reason := fmt.Sprintf("decisions: p_approve=%.2f", probability)
	if risk != "" {
		reason += ", risk: " + risk
	}
	if probability >= minConfidence {
		return agentdomain.JudgeVerdict{ID: id, Decision: agentdomain.JudgeDecisionApproved, Reason: reason}
	}
	return agentdomain.JudgeVerdict{ID: id, Decision: agentdomain.JudgeDecisionRejected, Reason: fmt.Sprintf("rejected by the judge (%s, below min_confidence %.2f)", reason, minConfidence)}
}

func riskLabel(decisions decisiondomain.Decisions) string {
	answer, ok := decisions.ByName(riskQuestionName)
	if !ok {
		return ""
	}
	return answer.Choice
}

func decisionUsage(decisions decisiondomain.Decisions) *sdk.CompletionUsage {
	if decisions.InputTokens == 0 {
		return nil
	}
	return &sdk.CompletionUsage{PromptTokens: int64(decisions.InputTokens), TotalTokens: int64(decisions.InputTokens)}
}
