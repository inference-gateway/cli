package config

// ResolveApprovalDelivery returns the effective ApprovalBehaviour* for a tool
// that needs approval, so chat and the headless executor agree on one rule.
// "block" always rejects, "judge" is always reachable, "ipc" needs a broker.
// "prompt" and unknown values prompt in chat, use IPC when a broker is attached,
// and block otherwise.
func ResolveApprovalDelivery(behaviour string, brokerAttached, isChat bool) string {
	switch behaviour {
	case ApprovalBehaviourBlock:
		return ApprovalBehaviourBlock
	case ApprovalBehaviourJudge:
		return ApprovalBehaviourJudge
	case ApprovalBehaviourIPC:
		if brokerAttached {
			return ApprovalBehaviourIPC
		}
		return ApprovalBehaviourBlock
	default:
		if isChat {
			return ApprovalBehaviourPrompt
		}
		if brokerAttached {
			return ApprovalBehaviourIPC
		}
		return ApprovalBehaviourBlock
	}
}
