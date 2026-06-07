package config

// ResolveApprovalDelivery decides the effective action for a tool that needs
// approval, given the configured tools.safety.approval_behaviour, whether an IPC
// approval broker is attached (headless under the channel manager, i.e.
// `--require-approval`), and whether we are in interactive chat. It returns one of
// the ApprovalBehaviour* values (prompt | ipc | block) - the same vocabulary as
// the config - applying the "channel not reachable -> block" fallback so the chat
// policy and the headless executor agree on a single safe rule.
//
//   - "block": always reject.
//   - "ipc": deliver over IPC if a broker is attached, otherwise block (there is
//     no IPC broker in chat, so this blocks there - a chat user should use the
//     default "prompt" instead).
//   - "prompt" (and any unrecognised value, which resolves to this safe default):
//     a TUI prompt in chat; otherwise IPC if a broker is attached (the channel
//     manager relays the prompt to the user); otherwise block (e.g. CI/heartbeat
//     with no approver).
func ResolveApprovalDelivery(behaviour string, brokerAttached, isChat bool) string {
	switch behaviour {
	case ApprovalBehaviourBlock:
		return ApprovalBehaviourBlock
	case ApprovalBehaviourIPC:
		if brokerAttached {
			return ApprovalBehaviourIPC
		}
		return ApprovalBehaviourBlock
	default: // prompt, plus any unrecognised value -> safe default
		if isChat {
			return ApprovalBehaviourPrompt
		}
		if brokerAttached {
			return ApprovalBehaviourIPC
		}
		return ApprovalBehaviourBlock
	}
}
