package headless

import (
	"cmp"
	"context"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	llm "github.com/inference-gateway/cli/internal/platform/llm"
)

type completionResultFrame struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Content   string `json:"content"`
	Reasoning string `json:"reasoning"`
	Error     string `json:"error"`
}

// completions answers a client's model side call, such as a shortcut's snippet,
// with the worker's own gateway and model, so it runs wherever the session does.
type completions struct {
	write     frameWriter
	completer llm.Completer
	models    convdomain.ModelService
}

// run writes exactly one completion_result per request id. A request that
// names no model runs on the worker's current one.
func (c *completions) run(msg panelFrame) {
	resp, err := c.completer.Complete(context.Background(), llm.Request{
		Model:     cmp.Or(msg.Model, c.models.GetCurrentModel()),
		System:    msg.System,
		Prompt:    msg.Prompt,
		MaxTokens: msg.MaxTokens,
	})
	result := completionResultFrame{Type: outboundCompletionResult, ID: msg.ID, Content: resp.Content, Reasoning: resp.Reasoning}
	if err != nil {
		result.Error = err.Error()
	}
	c.write(result)
}
