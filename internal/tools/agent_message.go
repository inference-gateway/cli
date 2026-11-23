package tools

import (
	"cmp"
	"context"
	"fmt"
	"strings"
	"time"

	uuid "github.com/google/uuid"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

// allowedSubagentKeys is the set of named tmux keys a message may press in an
// interactive subagent's pane. The list keeps a model from injecting arbitrary
// tmux key-specs or send-keys options through the `keys` argument.
var allowedSubagentKeys = map[string]bool{
	"Enter": true, "Escape": true, "Tab": true, "Space": true, "BSpace": true,
	"Up": true, "Down": true, "Left": true, "Right": true,
	"Home": true, "End": true, "PageUp": true, "PageDown": true,
}

const allowedSubagentKeyList = "Enter, Escape, Tab, Space, BSpace, Up, Down, Left, Right, Home, End, PageUp, PageDown"

// AgentMessageResult is the payload of an Agent call that sent a message: who
// it went to and what the sender is told about it.
type AgentMessageResult struct {
	To      string `json:"to"`
	Message string `json:"message"`
}

// isMessage reports whether an Agent call sends a message rather than spawning
// subagents.
func isMessage(args map[string]any) bool {
	return strings.TrimSpace(optionalString(args, "to")) != ""
}

// validateMessage checks the arguments of an Agent call that sends a message.
func validateMessage(args map[string]any) error {
	if _, spawns := args["tasks"]; spawns || optionalString(args, "description") != "" {
		return fmt.Errorf("'to' sends a message and cannot be combined with 'tasks' or 'description', which spawn subagents")
	}
	keys := optionalStringSlice(args, "keys")
	if strings.TrimSpace(optionalString(args, "message")) == "" && len(keys) == 0 {
		return fmt.Errorf("provide the 'message' to send, or 'keys' to press in an interactive subagent's pane")
	}
	for _, k := range keys {
		if !allowedSubagentKeys[k] {
			return fmt.Errorf("unsupported key %q; allowed keys: %s", k, allowedSubagentKeyList)
		}
	}
	return nil
}

// sendMessage delivers a message to one of this agent's own subagents or the
// agents it messaged, hands it to this agent's lead, which keeps it or relays it
// to the teammate it names, or, with no lead, starts the agent it names.
func (t *AgentTool) sendMessage(ctx context.Context, args map[string]any, start time.Time) *agentdomain.ToolExecutionResult {
	if err := validateMessage(args); err != nil {
		return t.errorResult(args, start, err.Error())
	}
	to := strings.TrimSpace(optionalString(args, "to"))
	text := optionalString(args, "message")
	keys := optionalStringSlice(args, "keys")
	submit := true
	if v, ok := args["submit"].(bool); ok {
		submit = v
	}

	if s := t.findSubagent(to, ""); s != nil {
		if s.Peer {
			text = teamNote(t.selfName(), text)
		}
		msg, err := t.deliver(ctx, s, text, keys, submit)
		if err != nil {
			return t.errorResult(args, start, err.Error())
		}
		return t.messageResult(args, start, to, msg)
	}

	if len(keys) > 0 || !submit {
		return t.errorResult(args, start, "'keys' and submit=false drive the pane of a subagent you spawned. Send a teammate, your lead or another agent a 'message' instead.")
	}
	lead := scheddomain.TeamChannelFrom(ctx)
	if lead == nil {
		agent, err := findMarkdownAgent(t.mdAgents, to)
		if err != nil {
			return t.errorResult(args, start, t.unknownRecipient(to))
		}
		return t.messagePeer(ctx, args, start, agent, text)
	}
	if err := lead.Post(scheddomain.TeamMessage{To: to, Text: text}); err != nil {
		return t.errorResult(args, start, fmt.Sprintf("Failed to send the message to %s: %v", to, err))
	}
	return t.messageResult(args, start, to, fmt.Sprintf("Sent the message to %s. A reply arrives as [Message from %s] - carry on with your task meanwhile, or end your turn if you need the reply first.", to, to))
}

// messagePeer starts the agent `to` named in a session of its own on the
// daemon, as a chat opened as that agent would run, with the message as its
// first turn. It is followed like a teammate: its turns and replies come back
// here, and the next message to it continues the same session.
func (t *AgentTool) messagePeer(ctx context.Context, args map[string]any, start time.Time, agent markdownAgent, text string) *agentdomain.ToolExecutionResult {
	if !t.threadRouted() {
		return t.errorResult(args, start, fmt.Sprintf("Messaging agent %s starts it in a session of its own on the daemon - enable the daemon binding.", agent.name))
	}
	parentModel := agentdomain.GetModel(ctx)
	spec := AgentTaskSpec{
		Label:       agent.name,
		Description: teamNote(t.selfName(), text),
		Model:       cmp.Or(agent.model, t.resolveModel("", parentModel)),
		Agent:       agent.name,
		Workspace:   agent.workspace,
		Peer:        true,
	}
	if err := unservedModelError(spec, t.servedModels(ctx), t.mdAgents, parentModel); err != nil {
		return t.errorResult(args, start, err.Error())
	}
	sessionID := uuid.NewString()
	if !t.dispatchKeepAlive(spec, scheddomain.SubagentModeHeadless, sessionID, agentdomain.GetSessionID(ctx)) {
		return t.errorResult(args, start, fmt.Sprintf("Failed to start agent %s: no job supervisor is wired.", agent.name))
	}
	return t.messageResult(args, start, agent.name, fmt.Sprintf("Started agent %s in a session of its own (%s) with your message. Its replies arrive as [Message from %s] - carry on with your task meanwhile, or end your turn if you need the reply first.", agent.name, sessionID, agent.name))
}

// selfName is the name this agent's messages carry: the agent its session runs
// as, else "lead".
func (t *AgentTool) selfName() string {
	return cmp.Or(t.config.Subagent.Agent, scheddomain.TeamLead)
}

// unknownRecipient explains that `to` names nobody this agent can reach.
func (t *AgentTool) unknownRecipient(to string) string {
	names := t.teamNames("")
	if len(names) == 0 {
		return fmt.Sprintf("No agent named %q: you have no lead and no subagents to message.", to)
	}
	return fmt.Sprintf("No agent named %q. You can message: %s.", to, strings.Join(names, ", "))
}

// deliver hands a subagent a message: the next turn of a headless one, or text
// and named keys typed into an interactive one's pane. A submitted turn re-arms
// its watcher, so the sender hears when it finishes. It returns what the sender
// is told.
func (t *AgentTool) deliver(ctx context.Context, s *scheddomain.SubagentState, text string, keys []string, submit bool) (string, error) {
	label := teammateName(s)
	if s.Mode != scheddomain.SubagentModeInteractive || s.PaneID == "" {
		return deliverHeadless(s, label, text, keys, submit)
	}
	if t.paneState(ctx, s.PaneID) == paneGone {
		return "", fmt.Errorf("subagent %s's pane no longer exists; it cannot receive input", label)
	}
	send := keys
	if submit {
		send = append(send, "Enter")
	}
	if err := t.sendKeys(ctx, s.PaneID, text, send); err != nil {
		return "", fmt.Errorf("failed to send input to subagent %s: %w", label, err)
	}
	msg := fmt.Sprintf("Sent input to subagent %s.", label)
	switch {
	case submit && rearmSubagent(t.tracker, s):
		msg += " It is now running again - you will be notified automatically when it finishes; do not poll."
	case !submit:
		msg += " Use ReadSubagentScreen to see the result."
	}
	return msg, nil
}

// deliverHeadless hands a headless subagent its next turn's message. Keys and
// submit=false drive a TUI, which a headless subagent has none of.
func deliverHeadless(s *scheddomain.SubagentState, label, text string, keys []string, submit bool) (string, error) {
	if len(keys) > 0 || !submit {
		return "", fmt.Errorf("subagent %s is headless: 'keys' and submit=false drive an interactive pane's TUI and are interactive-only. Send it a 'message' instead", label)
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("subagent %s is headless and takes a 'message'", label)
	}
	if s.Input == nil {
		return "", fmt.Errorf("subagent %s is a blocking or finished headless subagent and accepts no message", label)
	}
	if err := s.Input(text); err != nil {
		return "", fmt.Errorf("failed to send the message to subagent %s: %w", label, err)
	}
	return fmt.Sprintf("Sent the message to headless subagent %s. It runs it as its next turn - you will be notified automatically when that turn finishes; do not poll.", label), nil
}

func (t *AgentTool) messageResult(args map[string]any, start time.Time, to, msg string) *agentdomain.ToolExecutionResult {
	return &agentdomain.ToolExecutionResult{
		ToolName:  ToolAgent,
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      AgentMessageResult{To: to, Message: msg},
	}
}
