package loop

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	formatting "github.com/inference-gateway/cli/internal/platform/formatting"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	streamevent "github.com/inference-gateway/cli/internal/platform/streamevent"
)

// Judge input bounds: the intents are the first and latest user messages and
// each action a pending tool call; all are truncated so one giant payload
// cannot blow up the judge call. 2000 matches the summarizer's per-message
// cap. A turn with more than maxJudgeBatchSize gated calls is judged in
// several requests so one huge turn cannot blow the judge's context.
const (
	maxJudgeIntentLength = 2000
	maxJudgeActionLength = 4000
	maxJudgeBatchSize    = 20
)

// LLMJudge implements agentdomain.JudgeApprover with one no-tools
// GenerateContent call per batch (same shape as GenerateLLMSummary: direct
// client call, SkipMCP, its own timeout, bounded max_tokens). The judge.yaml
// on_error policy decides what a failed call means: deny (default) fails
// closed with a distinguishable reason - the same default as the no-approver
// block path - allow approves.
type LLMJudge struct {
	client sdk.Client
	config *config.Config
}

// NewLLMJudge creates the judge approver. Config validation fails fast at
// startup when no model is resolvable, so an empty model here is a hard error.
// judge.gateway_url gives the judge its own client (same API key and timeout
// as the agent's) so it can answer from a different gateway than the driver.
func NewLLMJudge(client sdk.Client, cfg *config.Config) *LLMJudge {
	if url := cfg.Judge.GatewayURL; url != "" {
		if !strings.HasSuffix(url, "/v1") {
			url = strings.TrimSuffix(url, "/") + "/v1"
		}
		client = sdk.NewClient(&sdk.ClientOptions{
			BaseURL: url,
			APIKey:  cfg.Gateway.APIKey,
			Timeout: time.Duration(cmp.Or(cfg.Client.Timeout, 200)) * time.Second,
		})
	}
	return &LLMJudge{client: client, config: cfg}
}

// Judge decides the pending tool calls: does each serve the user's intent and
// is it safe to run? Calls are judged in batches of maxJudgeBatchSize, one
// request each, and the verdicts come back in input order. The verdict
// contract is enforced by agentdomain.ParseJudgeVerdicts.
func (j *LLMJudge) Judge(ctx context.Context, in agentdomain.JudgeInput) (agentdomain.JudgeVerdicts, error) {
	model := in.Model
	if model == "" {
		return agentdomain.JudgeVerdicts{}, fmt.Errorf("no judge model configured: set judge.model in %s or agent.model", config.DefaultJudgePath)
	}

	provider, modelName := config.ParseModel(model)
	if provider == "" {
		return agentdomain.JudgeVerdicts{}, fmt.Errorf("invalid judge model format %q, expected 'provider/model'", model)
	}

	out := agentdomain.JudgeVerdicts{Verdicts: make([]agentdomain.JudgeVerdict, 0, len(in.Actions))}
	for batch := range slices.Chunk(in.Actions, maxJudgeBatchSize) {
		verdicts, usage := j.judgeBatch(ctx, sdk.Provider(provider), modelName, in, batch)
		out.Verdicts = append(out.Verdicts, verdicts...)
		out.Usage = addUsage(out.Usage, usage)
	}
	return out, nil
}

// judgeBatch asks the judge once for every action in batch and returns one
// verdict per action in order. A failed request or unparseable answer applies
// on_error to the whole batch; an answer that omits a call applies on_error
// to that call only.
func (j *LLMJudge) judgeBatch(ctx context.Context, provider sdk.Provider, modelName string, in agentdomain.JudgeInput, batch []agentdomain.JudgeAction) ([]agentdomain.JudgeVerdict, *sdk.CompletionUsage) {
	jcfg := j.config.Judge.Effective()
	byID, usage, err := j.askJudge(ctx, provider, modelName, in, batch, jcfg)
	verdicts := make([]agentdomain.JudgeVerdict, len(batch))
	for i, id := range actionIDs(batch) {
		if err != nil {
			verdicts[i] = j.onError(id, err, jcfg.OnError)
			continue
		}
		verdict, ok := byID[id]
		if !ok {
			verdict = j.onError(id, fmt.Errorf("judge gave no verdict for call %s", id), jcfg.OnError)
		}
		verdicts[i] = verdict
	}
	return verdicts, usage
}

// askJudge renders the prompt for batch, makes the GenerateContent call and
// parses the verdicts keyed by id.
func (j *LLMJudge) askJudge(ctx context.Context, provider sdk.Provider, modelName string, in agentdomain.JudgeInput, batch []agentdomain.JudgeAction, jcfg config.JudgeConfig) (map[string]agentdomain.JudgeVerdict, *sdk.CompletionUsage, error) {
	prompt := strings.NewReplacer(
		"{root_intent}", truncateForJudge(in.RootIntent, maxJudgeIntentLength),
		"{intent}", truncateForJudge(in.Intent, maxJudgeIntentLength),
		"{action}", renderJudgeActions(batch),
	).Replace(jcfg.Prompt)
	messages := []sdk.Message{
		{Role: sdk.System, Content: sdk.NewMessageContent(jcfg.SystemPrompt)},
		{Role: sdk.User, Content: sdk.NewMessageContent(prompt)},
	}
	streamevent.EmitDebugEvent("judge_request", map[string]any{
		"model": in.Model, "system": jcfg.SystemPrompt, "prompt": prompt, "calls": len(batch),
	})

	jctx, cancel := context.WithTimeout(ctx, time.Duration(jcfg.Timeout)*time.Second)
	defer cancel()

	response, err := j.client.
		WithOptions(&sdk.CreateChatCompletionRequest{MaxTokens: &jcfg.MaxTokens}).
		WithMiddlewareOptions(&sdk.MiddlewareOptions{SkipMCP: true}).
		GenerateContent(jctx, provider, modelName, messages)
	if err != nil {
		return nil, nil, fmt.Errorf("judge call failed: %w", err)
	}
	if len(response.Choices) == 0 {
		return nil, response.Usage, fmt.Errorf("judge returned no choices")
	}
	raw, err := response.Choices[0].Message.Content.AsMessageContent0()
	if err != nil {
		return nil, response.Usage, fmt.Errorf("extracting judge content: %w", err)
	}
	if response.Choices[0].FinishReason == sdk.Length && strings.TrimSpace(raw) == "" {
		return nil, response.Usage, fmt.Errorf("judge spent all %d max_tokens before answering (reasoning models think against this budget); raise judge.max_tokens", jcfg.MaxTokens)
	}

	byID, err := agentdomain.ParseJudgeVerdicts(raw, actionIDs(batch))
	return byID, response.Usage, err
}

func actionIDs(batch []agentdomain.JudgeAction) []string {
	ids := make([]string, len(batch))
	for i, action := range batch {
		ids[i] = action.ID
	}
	return ids
}

// onError applies judge.on_error to a call the judge failed to decide. deny
// rejects with a distinguishable "judge unavailable" reason so the driver can
// retry or route around; allow approves. The error itself is swallowed: the
// verdict carries everything downstream needs.
func (j *LLMJudge) onError(id string, err error, onError string) agentdomain.JudgeVerdict {
	reason := fmt.Sprintf("judge unavailable: %v", err)
	logger.Warn("judge call failed, applying on_error policy", "on_error", onError, "call", id, "error", err)
	if onError == config.JudgeOnErrorAllow {
		return agentdomain.JudgeVerdict{ID: id, Decision: agentdomain.JudgeDecisionApproved, Reason: reason}
	}
	return agentdomain.JudgeVerdict{ID: id, Decision: agentdomain.JudgeDecisionRejected, Reason: reason}
}

// addUsage sums judge request usage across batches so a turn's judge spend
// is recorded once.
func addUsage(total, usage *sdk.CompletionUsage) *sdk.CompletionUsage {
	if usage == nil {
		return total
	}
	if total == nil {
		sum := *usage
		return &sum
	}
	total.PromptTokens += usage.PromptTokens
	total.CompletionTokens += usage.CompletionTokens
	total.TotalTokens += usage.TotalTokens
	return total
}

// renderJudgeActions renders every pending call as its own tagged block so
// the judge can name each verdict by id. Each action is capped separately.
func renderJudgeActions(batch []agentdomain.JudgeAction) string {
	blocks := make([]string, len(batch))
	for i, action := range batch {
		blocks[i] = fmt.Sprintf("<tool_call id=%q>\n%s\n</tool_call>", action.ID, truncateForJudge(action.Action, maxJudgeActionLength))
	}
	return strings.Join(blocks, "\n\n")
}

// truncateForJudge caps a judge input, marking the cut like the summarizer does.
func truncateForJudge(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "... [truncated]"
}

// userIntents returns the first and the latest non-hidden user messages of
// the conversation: the session's root task and the most recent ask (which
// may be a bare "continue" that only makes sense next to the root). With a
// single user message root is empty so the judge is not shown it twice. With
// only hidden user messages (a session started by plan approval) the first is used.
func userIntents(repo convdomain.ConversationRepository) (root, latest string) {
	if repo == nil {
		return "", ""
	}
	var firstHidden string
	for _, entry := range repo.GetMessages() {
		if entry.Message.Role != sdk.User {
			continue
		}
		content := formatting.ExtractTextFromContent(entry.Message.Content, nil)
		if strings.TrimSpace(content) == "" {
			continue
		}
		if entry.Hidden {
			firstHidden = cmp.Or(firstHidden, content)
			continue
		}
		if root == "" {
			root = content
			continue
		}
		latest = content
	}
	if latest == "" {
		return "", cmp.Or(root, firstHidden)
	}
	return root, latest
}

// judgeAction renders the pending tool call (name + arguments) for the judge
// prompt.
func judgeAction(tc sdk.ChatCompletionMessageToolCall) agentdomain.JudgeAction {
	return agentdomain.JudgeAction{ID: tc.ID, Action: fmt.Sprintf("%s: %s", tc.Function.Name, tc.Function.Arguments)}
}
