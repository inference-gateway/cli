package services

import (
	sdk "github.com/inference-gateway/sdk"
)

// CancelledToolResponseContent is the sentinel body used for synthetic
// tool responses inserted by EnsureToolCallsClosed. The wording is
// surfaced to the model in the next turn and to the user via the
// conversation view, so it must be self-explanatory and stable.
const CancelledToolResponseContent = "[Tool execution cancelled by user before completion]"

// SyntheticToolResponse pairs a synthesized Tool-role message with the
// metadata of the assistant tool_call it closes. Callers persist
// Message to the conversation repository and use ToolCallID / ToolName
// when emitting UI events.
type SyntheticToolResponse struct {
	Message    sdk.Message
	ToolCallID string
	ToolName   string
}

// EnsureToolCallsClosed enforces the OpenAI-style invariant that every
// assistant message with tool_calls is followed by a Tool-role message
// for each tool_call_id. For every unmatched tool_call_id it inserts a
// synthetic Tool-role response with CancelledToolResponseContent at the
// tail of the existing tool responses for that assistant turn,
// preserving the original tool_calls order.
//
// The returned conversation is a copy; the input slice is not mutated.
// The returned synthetics list is in insertion order and is non-nil
// only when at least one synthetic was added.
//
// Idempotent: running on an already-repaired conversation returns the
// input shape with an empty synthetics slice.
func EnsureToolCallsClosed(conv []sdk.Message) ([]sdk.Message, []SyntheticToolResponse) {
	if len(conv) == 0 {
		return conv, nil
	}

	var synthetics []SyntheticToolResponse
	repaired := make([]sdk.Message, 0, len(conv))

	for i := 0; i < len(conv); i++ {
		msg := conv[i]
		repaired = append(repaired, msg)

		if msg.Role != sdk.Assistant || msg.ToolCalls == nil || len(*msg.ToolCalls) == 0 {
			continue
		}

		seen := make(map[string]bool, len(*msg.ToolCalls))
		j := i + 1
		for j < len(conv) && conv[j].Role == sdk.Tool {
			if conv[j].ToolCallID != nil {
				seen[*conv[j].ToolCallID] = true
			}
			repaired = append(repaired, conv[j])
			j++
		}

		for _, tc := range *msg.ToolCalls {
			if tc.ID == "" || seen[tc.ID] {
				continue
			}
			id := tc.ID
			synth := sdk.Message{
				Role:       sdk.Tool,
				Content:    sdk.NewMessageContent(CancelledToolResponseContent),
				ToolCallID: &id,
			}
			repaired = append(repaired, synth)
			synthetics = append(synthetics, SyntheticToolResponse{
				Message:    synth,
				ToolCallID: tc.ID,
				ToolName:   tc.Function.Name,
			})
		}

		i = j - 1
	}

	return repaired, synthetics
}
