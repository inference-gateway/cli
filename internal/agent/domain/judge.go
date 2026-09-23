// LLM-judge decision values, reported on JudgeVerdictChatEvent when the judge
// delivery decides approval-requiring tool calls (approval_behaviour "judge" /
// agent mode auto-with-judge).

package domain

// JudgeDecision is the verdict value the LLM judge returns for one pending
// tool call.
type JudgeDecision string

// Judge verdict decisions. The judge must answer with exactly one JSON object
// {"decision": "<one of these literals>", "reason": "<short text>"}.
const (
	JudgeDecisionApproved JudgeDecision = "approved"
	JudgeDecisionRejected JudgeDecision = "rejected"
)
