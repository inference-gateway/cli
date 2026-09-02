// LLM-judge approval value types: the parsed verdict of the judge that
// decides approval-requiring tool calls when the judge delivery is selected
// (approval_behaviour "judge" / agent mode auto-with-judge).

package domain

import (
	"encoding/json"
	"fmt"
	"strings"

	sdk "github.com/inference-gateway/sdk"
)

// JudgeDecision is the verdict value the LLM judge returns for one pending
// tool call.
type JudgeDecision string

// Judge verdict decisions. The judge must answer with exactly one JSON object
// {"decision": "<one of these literals>", "reason": "<short text>"}.
const (
	JudgeDecisionApproved JudgeDecision = "approved"
	JudgeDecisionRejected JudgeDecision = "rejected"
)

// JudgeVerdict is the parsed decision of the LLM judge for one pending tool
// call. Decision is always one of the JudgeDecision values once parsed;
// nothing downstream reads the judge's raw output.
type JudgeVerdict struct {
	Decision JudgeDecision
	Reason   string
	// Usage is the judge call's token usage (nil when the call never
	// completed) so the session totals include it.
	Usage *sdk.CompletionUsage `json:"-"`
}

// Approved reports whether the judge approved the action.
func (v JudgeVerdict) Approved() bool {
	return v.Decision == JudgeDecisionApproved
}

// ParseJudgeVerdict extracts the verdict JSON object from the judge's raw
// output. Keeping only the outermost {...} drops code fences and prose; it requires
// decision to be one of the two literals, and rejects anything else so a
// malformed judge response flows into on_error handling.
func ParseJudgeVerdict(raw string) (JudgeVerdict, error) {
	trimmed := strings.TrimSpace(raw)

	// Keep only the outermost JSON object, ignoring surrounding prose.
	start := strings.IndexByte(trimmed, '{')
	end := strings.LastIndexByte(trimmed, '}')
	if start < 0 || end <= start {
		return JudgeVerdict{}, fmt.Errorf("judge returned no JSON object: %.200s", strings.TrimSpace(raw))
	}

	var verdict JudgeVerdict
	if err := json.Unmarshal([]byte(trimmed[start:end+1]), &verdict); err != nil {
		return JudgeVerdict{}, fmt.Errorf("parsing judge verdict: %w", err)
	}

	switch verdict.Decision {
	case JudgeDecisionApproved, JudgeDecisionRejected:
	default:
		return JudgeVerdict{}, fmt.Errorf("judge decision %q: must be %q or %q", verdict.Decision, JudgeDecisionApproved, JudgeDecisionRejected)
	}

	return verdict, nil
}
