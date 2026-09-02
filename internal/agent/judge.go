package agent

import (
	"cmp"
	"context"
	"fmt"
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	streamevent "github.com/inference-gateway/cli/internal/platform/streamevent"
)

// Judge input bounds: the intent is the latest user message and the action the
// pending tool call; both are truncated so one giant payload cannot blow up the
// judge call. 2000 matches the summarizer's per-message cap.
const (
	maxJudgeIntentLength = 2000
	maxJudgeActionLength = 4000
)

// LLMJudge implements agentdomain.JudgeApprover with one no-tools GenerateContent
// call (same shape as GenerateLLMSummary: direct client call, SkipMCP, its own
// timeout, bounded max_tokens). The judge.yaml on_error policy decides what a
// failed call means: deny (default) fails closed with a distinguishable reason -
// the same default as the no-approver block path - allow approves.
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

// Judge decides one pending tool call: does it serve the user's intent and is
// it safe to run? The verdict contract is enforced by ParseJudgeVerdict.
func (j *LLMJudge) Judge(ctx context.Context, model, intent, action string) (agentdomain.JudgeVerdict, error) {
	if model == "" {
		return agentdomain.JudgeVerdict{}, fmt.Errorf("no judge model configured: set judge.model in %s or agent.model", config.DefaultJudgePath)
	}

	slashIndex := strings.Index(model, "/")
	if slashIndex == -1 {
		return agentdomain.JudgeVerdict{}, fmt.Errorf("invalid judge model format %q, expected 'provider/model'", model)
	}

	jcfg := j.config.Judge.Effective()
	prompt := strings.NewReplacer(
		"{intent}", truncateForJudge(intent, maxJudgeIntentLength),
		"{action}", truncateForJudge(action, maxJudgeActionLength),
	).Replace(jcfg.Prompt)
	messages := []sdk.Message{
		{Role: sdk.System, Content: sdk.NewMessageContent(jcfg.SystemPrompt)},
		{Role: sdk.User, Content: sdk.NewMessageContent(prompt)},
	}
	streamevent.EmitDebugEvent("judge_request", map[string]any{
		"model": model, "system": jcfg.SystemPrompt, "prompt": prompt,
	})

	jctx, cancel := context.WithTimeout(ctx, time.Duration(jcfg.Timeout)*time.Second)
	defer cancel()

	response, err := j.client.
		WithOptions(&sdk.CreateChatCompletionRequest{MaxTokens: &jcfg.MaxTokens}).
		WithMiddlewareOptions(&sdk.MiddlewareOptions{SkipMCP: true}).
		GenerateContent(jctx, sdk.Provider(model[:slashIndex]), model[slashIndex+1:], messages)
	if err != nil {
		return j.onError(fmt.Errorf("judge call failed: %w", err), jcfg.OnError)
	}
	if len(response.Choices) == 0 {
		return j.onError(fmt.Errorf("judge returned no choices"), jcfg.OnError)
	}
	raw, err := response.Choices[0].Message.Content.AsMessageContent0()
	if err != nil {
		return j.onError(fmt.Errorf("extracting judge content: %w", err), jcfg.OnError)
	}

	verdict, parseErr := agentdomain.ParseJudgeVerdict(raw)
	if parseErr != nil {
		return j.onError(parseErr, jcfg.OnError)
	}
	verdict.Usage = response.Usage
	return verdict, nil
}

// onError applies judge.on_error to a failed judge call. deny rejects with a
// distinguishable "judge unavailable" reason so the driver can retry or route
// around; allow approves. The error itself is swallowed: the verdict carries
// everything downstream needs.
func (j *LLMJudge) onError(err error, onError string) (agentdomain.JudgeVerdict, error) {
	reason := fmt.Sprintf("judge unavailable: %v", err)
	logger.Warn("judge call failed, applying on_error policy", "on_error", onError, "error", err)
	if onError == config.JudgeOnErrorAllow {
		return agentdomain.JudgeVerdict{Decision: agentdomain.JudgeDecisionApproved, Reason: reason}, nil
	}
	return agentdomain.JudgeVerdict{Decision: agentdomain.JudgeDecisionRejected, Reason: reason}, nil
}

// truncateForJudge caps a judge input, marking the cut like the summarizer does.
func truncateForJudge(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "... [truncated]"
}

// latestUserIntent returns the latest non-hidden user message from the
// conversation: the task in headless, the last human message in chat.
func latestUserIntent(repo convdomain.ConversationRepository) string {
	if repo == nil {
		return ""
	}
	entries := repo.GetMessages()
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry.Hidden || entry.Message.Role != sdk.User {
			continue
		}
		content, err := entry.Message.Content.AsMessageContent0()
		if err != nil || strings.TrimSpace(content) == "" {
			continue
		}
		return content
	}
	return ""
}

// judgeActionInput renders the pending tool call (name + arguments) for the
// judge prompt.
func judgeActionInput(tc sdk.ChatCompletionMessageToolCall) string {
	return fmt.Sprintf("%s: %s", tc.Function.Name, tc.Function.Arguments)
}
