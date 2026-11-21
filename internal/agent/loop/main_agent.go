package loop

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	states "github.com/inference-gateway/cli/internal/agent/loop/states"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// errSwitchDuringRun refuses a main agent switch while a turn runs, since the
// agent's instructions and tools apply from the next turn on.
var errSwitchDuringRun = errors.New("finish or stop the running turn before switching the main agent")

// mainAgentMarker names, in an agent-change reminder, the main agent the model
// was told to be. The conversation's last one is the agent it runs as.
var mainAgentMarker = regexp.MustCompile(`(?m)^Main agent: ([a-z0-9_-]+)$`)

// mainAgentState is the main agent of one conversation and the agent-change
// reminder its next turn still has to append.
type mainAgentState struct {
	conversation string
	agent        agentdomain.MainAgent
	reminder     string
}

// SetSelectModelFn wires how a switch to an agent that pins a model selects it.
func (s *Agent) SetSelectModelFn(fn func(model string) error) {
	s.selectModel = fn
}

// SetMainAgentResolver wires how a conversation resumed after a switch finds
// the tools and MCP servers of the agent its history names, and the agent a
// chat starts as. An empty launchAgent is the default main agent.
func (s *Agent) SetMainAgentResolver(launchAgent string, resolve func(name string) (agentdomain.MainAgent, error)) {
	s.launchAgent = launchAgent
	s.resolveMainAgent = resolve
}

// SelectMainAgent switches the conversation's main agent for its next turns.
// The system prompt and tool definitions stay as they are, so the prompt cache
// holds: the agent's instructions are appended as a reminder, and its tool
// policy is checked when a tool runs. Switching to the agent the model was last
// told to be appends nothing.
func (s *Agent) SelectMainAgent(agent agentdomain.MainAgent) error {
	if s.hasActiveRun() {
		return errSwitchDuringRun
	}
	agent, err := s.completeNamedAgent(agent)
	if err != nil {
		return err
	}
	if agent.Model != "" && s.selectModel != nil {
		if err := s.selectModel(agent.Model); err != nil {
			return fmt.Errorf("selecting the model of agent %s: %w", agent.Name, err)
		}
	}
	s.mainAgentMu.Lock()
	defer s.mainAgentMu.Unlock()
	state := s.mainAgentStateLocked()
	state.agent = agent
	state.reminder = ""
	if agent.Name != s.toldMainAgent() {
		state.reminder = agentChangeReminder(agent)
	}
	s.mainAgentState = state
	return nil
}

// completeNamedAgent resolves an agent picked by its name alone from the
// project's agent files. One a daemon resolved arrives complete.
func (s *Agent) completeNamedAgent(agent agentdomain.MainAgent) (agentdomain.MainAgent, error) {
	if agent.Instructions != "" || s.resolveMainAgent == nil {
		return agent, nil
	}
	return s.resolveMainAgent(agent.Name)
}

// MainAgent is the agent the current conversation runs as.
func (s *Agent) MainAgent() agentdomain.MainAgent {
	s.mainAgentMu.Lock()
	defer s.mainAgentMu.Unlock()
	return s.mainAgentStateLocked().agent
}

// mainAgentStateLocked is the current conversation's state, recovered from its
// history the first time the conversation is seen. The caller holds mainAgentMu.
func (s *Agent) mainAgentStateLocked() mainAgentState {
	conversation := s.currentConversationID()
	if s.mainAgentState.agent.Name != "" && s.mainAgentState.conversation == conversation {
		return s.mainAgentState
	}
	s.mainAgentState = mainAgentState{conversation: conversation, agent: s.recoverMainAgent()}
	return s.mainAgentState
}

// recoverMainAgent is the agent the conversation's history last told the model
// to be, else the agent the chat started as.
func (s *Agent) recoverMainAgent() agentdomain.MainAgent {
	name := s.toldMainAgent()
	if name == s.startingAgent() || s.resolveMainAgent == nil {
		return agentdomain.MainAgent{Name: name}
	}
	agent, err := s.resolveMainAgent(name)
	if err != nil {
		logger.Warn("resumed conversation names a main agent this computer cannot resolve, running it without its tool policy",
			"agent", name, "error", err)
		return agentdomain.MainAgent{Name: name}
	}
	return agent
}

// toldMainAgent is the main agent the conversation's last agent-change
// reminder named, else the agent the chat started as.
func (s *Agent) toldMainAgent() string {
	if s.conversationRepo == nil {
		return s.startingAgent()
	}
	messages := s.conversationRepo.GetMessages()
	for i := len(messages) - 1; i >= 0; i-- {
		if name := markedMainAgent(messages[i]); name != "" {
			return name
		}
	}
	return s.startingAgent()
}

// startingAgent is the agent a chat runs as before any switch.
func (s *Agent) startingAgent() string {
	if s.launchAgent != "" {
		return s.launchAgent
	}
	return agentdomain.DefaultMainAgentName
}

// markedMainAgent is the agent an agent-change reminder names, "" for any other
// message.
func markedMainAgent(entry convdomain.ConversationEntry) string {
	if !entry.Hidden || entry.Message.Role != sdk.User {
		return ""
	}
	text, err := entry.Message.Content.AsMessageContent0()
	if err != nil {
		return ""
	}
	if match := mainAgentMarker.FindStringSubmatch(text); match != nil {
		return match[1]
	}
	return ""
}

// agentChangeReminder tells the model it now runs as agent: what it is, the
// instructions to follow from here on, and the tools it may call.
func agentChangeReminder(agent agentdomain.MainAgent) string {
	var b strings.Builder
	b.WriteString("<system-reminder>\n")
	fmt.Fprintf(&b, "Main agent: %s\n", agent.Name)
	fmt.Fprintf(&b, "You are now the main agent %q. From here on, follow these instructions instead of any earlier agent's:\n\n", agent.Name)
	b.WriteString(strings.TrimSpace(agent.Instructions))
	if agent.Tools != nil {
		fmt.Fprintf(&b, "\n\nYou may call only these tools: %s.", strings.Join(agent.Tools, ", "))
	}
	b.WriteString("\n</system-reminder>")
	return b.String()
}

// injectMainAgentChange appends the pending agent-change reminder before the
// next stream and persists it, so a resumed conversation knows its agent.
func (s *Agent) injectMainAgentChange(agentCtx *states.AgentContext) {
	if conversationAwaitsToolResults(*agentCtx.Conversation) {
		return
	}
	s.mainAgentMu.Lock()
	state := s.mainAgentStateLocked()
	reminder := state.reminder
	s.mainAgentState.reminder = ""
	s.mainAgentMu.Unlock()
	if reminder == "" {
		return
	}
	msg := sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent(reminder)}
	*agentCtx.Conversation = append(*agentCtx.Conversation, msg)
	if s.conversationRepo == nil {
		return
	}
	if err := s.conversationRepo.AddMessage(convdomain.ConversationEntry{Message: msg, Time: time.Now(), Hidden: true}); err != nil {
		logger.Error("failed to store the main agent change", "error", err)
	}
}

// currentConversationID is the conversation the main agent belongs to.
func (s *Agent) currentConversationID() string {
	if s.conversationRepo == nil {
		return ""
	}
	return s.conversationRepo.GetCurrentConversationID()
}

// hasActiveRun reports whether a turn of this agent is running.
func (s *Agent) hasActiveRun() bool {
	s.sessionMux.RLock()
	defer s.sessionMux.RUnlock()
	return len(s.activeSessions) > 0
}
