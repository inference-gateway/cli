// LLM-judge decision values, reported on JudgeVerdictChatEvent when the judge
// delivery decides approval-requiring tool calls (approval_behaviour "judge" /
// agent mode auto-with-judge).

package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	sdk "github.com/inference-gateway/sdk"
)

// JudgeDecision is the verdict value the LLM judge returns for one pending
// tool call.
type JudgeDecision string

// Judge verdict decisions. The judge answers with a JSON array holding one
// {"id": "<tool call id>", "decision": "<one of these literals>", "reason": "<short text>"}
// object per pending tool call.
const (
	JudgeDecisionApproved JudgeDecision = "approved"
	JudgeDecisionRejected JudgeDecision = "rejected"
)

// JudgeAction is one pending tool call put to the judge: ID is the tool call
// id the verdict must name, Action the rendered call (name + arguments).
type JudgeAction struct {
	ID     string
	Action string
}

// JudgeInput is one approval question covering every gated call of a turn:
// Model is the resolved "provider/model" id that answers it, RootIntent the
// first non-hidden user message of the session, Intent the latest one, and
// Actions the pending tool calls.
type JudgeInput struct {
	Model      string
	RootIntent string
	Intent     string
	Actions    []JudgeAction
}

// JudgeVerdict is the parsed decision of the LLM judge for one pending tool
// call. Decision is always one of the JudgeDecision values once parsed.
// Nothing downstream reads the judge's raw output.
type JudgeVerdict struct {
	ID       string        `json:"id"`
	Decision JudgeDecision `json:"decision"`
	Reason   string        `json:"reason"`
}

// Approved reports whether the judge approved the action.
func (v JudgeVerdict) Approved() bool {
	return v.Decision == JudgeDecisionApproved
}

// JudgeVerdicts is the judge's answer to one JudgeInput: one verdict per
// action, in the same order, and the token usage of the request(s) behind it.
type JudgeVerdicts struct {
	Verdicts []JudgeVerdict
	Usage    *sdk.CompletionUsage
}

// JudgeApprover decides the pending tool calls of a turn by asking a small LLM
// whether each serves the user's intent and is safe. It is the approver
// selected by approval_behaviour "judge" / agent mode auto-with-judge: always
// reachable, so headless and CI get a real approver instead of blocking.
type JudgeApprover interface {
	Judge(ctx context.Context, in JudgeInput) (JudgeVerdicts, error)
}

// ParseJudgeVerdicts extracts the verdicts from the judge's raw output, keyed
// by tool call id. Keeping only the outermost [...] (or {...}) drops code
// fences and prose. A bare object without an id is accepted only for a
// single-call batch so prompts written for the old one-object contract keep
// working. Entries with an unknown id or decision are dropped so the caller
// fails closed per call. Only unparseable output is an error.
func ParseJudgeVerdicts(raw string, ids []string) (map[string]JudgeVerdict, error) {
	payload, err := outermostJSON(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}

	var verdicts []JudgeVerdict
	if strings.HasPrefix(payload, "[") {
		err = json.Unmarshal([]byte(payload), &verdicts)
	} else {
		var single JudgeVerdict
		err = json.Unmarshal([]byte(payload), &single)
		verdicts = []JudgeVerdict{single}
	}
	if err != nil {
		return nil, fmt.Errorf("parsing judge verdict: %w", err)
	}
	if len(verdicts) == 1 && verdicts[0].ID == "" && len(ids) == 1 {
		verdicts[0].ID = ids[0]
	}

	byID := make(map[string]JudgeVerdict, len(ids))
	for _, v := range verdicts {
		if !validDecision(v.Decision) || !slices.Contains(ids, v.ID) {
			continue
		}
		if _, dup := byID[v.ID]; !dup {
			byID[v.ID] = v
		}
	}
	return byID, nil
}

// outermostJSON returns the outermost JSON array or object of trimmed,
// whichever opens first.
func outermostJSON(trimmed string) (string, error) {
	open, closing := byte('['), byte(']')
	arrayStart, objectStart := strings.IndexByte(trimmed, '['), strings.IndexByte(trimmed, '{')
	if arrayStart < 0 || (objectStart >= 0 && objectStart < arrayStart) {
		open, closing = '{', '}'
	}
	start := strings.IndexByte(trimmed, open)
	end := strings.LastIndexByte(trimmed, closing)
	if start < 0 || end <= start {
		return "", fmt.Errorf("judge returned no JSON: %.200s", trimmed)
	}
	return trimmed[start : end+1], nil
}

func validDecision(d JudgeDecision) bool {
	return d == JudgeDecisionApproved || d == JudgeDecisionRejected
}
