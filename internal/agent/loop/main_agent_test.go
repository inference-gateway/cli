package loop

import (
	"errors"
	"slices"
	"strings"
	"testing"

	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	conversation "github.com/inference-gateway/cli/internal/conversation"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

var exploreAgent = agentdomain.MainAgent{Name: "explore", Instructions: "Read-only exploration agent.", Tools: []string{"Read", "Grep"}}

func newMainAgentService(repo convdomain.ConversationRepository) *Agent {
	cfg := &config.Config{}
	cfg.Prompts.Agent.SystemPrompt = "Base system prompt."
	return &Agent{config: cfg, conversationRepo: repo, activeSessions: make(map[string]*sessionCancel)}
}

func messageText(t *testing.T, msg sdk.Message) string {
	t.Helper()
	text, err := msg.Content.AsMessageContent0()
	if err != nil {
		t.Fatal(err)
	}
	return text
}

func TestSelectMainAgentOnlyAppends(t *testing.T) {
	repo := conversation.NewInMemoryConversationRepository(nil, nil)
	svc := newMainAgentService(repo)
	conv := []sdk.Message{
		{Role: sdk.System, Content: sdk.NewMessageContent("Base system prompt.")},
		{Role: sdk.User, Content: sdk.NewMessageContent("hi")},
	}
	prefix := slices.Clone(conv)
	systemPrompt := svc.BuildSystemPrompt()

	if err := svc.SelectMainAgent(exploreAgent); err != nil {
		t.Fatal(err)
	}
	svc.injectMainAgentChange(newReminderAgentCtx(&conv, 1, 0))

	if len(conv) != 3 || !slices.EqualFunc(conv[:2], prefix, func(a, b sdk.Message) bool { return messageText(t, a) == messageText(t, b) }) {
		t.Fatalf("a switch must leave the cached prefix as it was and append one message, got %d messages", len(conv))
	}
	if svc.BuildSystemPrompt() != systemPrompt {
		t.Fatal("a switch must not change message[0], or the prompt cache misses")
	}
	reminder := messageText(t, conv[2])
	for _, want := range []string{"Main agent: explore", "Read-only exploration agent.", "only these tools: Read, Grep"} {
		if !strings.Contains(reminder, want) {
			t.Errorf("reminder %q lacks %q", reminder, want)
		}
	}
	if stored := repo.GetMessages(); len(stored) != 1 || !stored[0].Hidden {
		t.Fatalf("the reminder must be stored as a hidden message, got %+v", stored)
	}
	if got := svc.MainAgent(); got.Name != "explore" || !slices.Equal(got.Tools, exploreAgent.Tools) {
		t.Fatalf("MainAgent = %+v, want explore", got)
	}

	svc.injectMainAgentChange(newReminderAgentCtx(&conv, 2, 0))
	if err := svc.SelectMainAgent(exploreAgent); err != nil {
		t.Fatal(err)
	}
	svc.injectMainAgentChange(newReminderAgentCtx(&conv, 3, 0))
	if len(conv) != 3 {
		t.Fatalf("the model was already told it is explore, but %d messages followed", len(conv)-3)
	}

	if err := svc.SelectMainAgent(agentdomain.MainAgent{Name: agentdomain.DefaultMainAgentName, Instructions: "Base system prompt."}); err != nil {
		t.Fatal(err)
	}
	svc.injectMainAgentChange(newReminderAgentCtx(&conv, 4, 0))
	if len(conv) != 4 || !strings.Contains(messageText(t, conv[3]), "Main agent: main") {
		t.Fatal("switching back to main must tell the model so")
	}
}

func TestSelectMainAgentWithoutAnyChangeAppendsNothing(t *testing.T) {
	svc := newMainAgentService(conversation.NewInMemoryConversationRepository(nil, nil))
	if err := svc.SelectMainAgent(agentdomain.MainAgent{Name: agentdomain.DefaultMainAgentName}); err != nil {
		t.Fatal(err)
	}
	conv := []sdk.Message{{Role: sdk.User, Content: sdk.NewMessageContent("hi")}}
	svc.injectMainAgentChange(newReminderAgentCtx(&conv, 1, 0))
	if len(conv) != 1 {
		t.Fatal("picking the agent the chat already runs as must append nothing")
	}
}

func TestSelectMainAgentRefusesDuringARun(t *testing.T) {
	svc := newMainAgentService(conversation.NewInMemoryConversationRepository(nil, nil))
	svc.activeSessions["run-1"] = &sessionCancel{}
	if err := svc.SelectMainAgent(exploreAgent); !errors.Is(err, errSwitchDuringRun) {
		t.Fatalf("err = %v, want the refusal while a turn runs", err)
	}
}

func TestSelectMainAgentSelectsTheModelItPins(t *testing.T) {
	svc := newMainAgentService(conversation.NewInMemoryConversationRepository(nil, nil))
	var selected []string
	svc.SetSelectModelFn(func(model string) error {
		selected = append(selected, model)
		return nil
	})
	if err := svc.SelectMainAgent(exploreAgent); err != nil {
		t.Fatal(err)
	}
	pinned := exploreAgent
	pinned.Model = "deepseek/deepseek-v4-flash"
	if err := svc.SelectMainAgent(pinned); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(selected, []string{"deepseek/deepseek-v4-flash"}) {
		t.Fatalf("selected models = %v, want only the pinned one", selected)
	}
}

func TestMainAgentRecoversFromTheConversationHistory(t *testing.T) {
	repo := &convmocks.FakeConversationRepository{}
	repo.GetCurrentConversationIDReturns("conv-2")
	repo.GetMessagesReturns([]convdomain.ConversationEntry{
		{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("hi")}},
		{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent(agentChangeReminder(exploreAgent))}, Hidden: true},
	})
	svc := newMainAgentService(repo)
	svc.SetMainAgentResolver("", func(name string) (agentdomain.MainAgent, error) {
		if name != "explore" {
			return agentdomain.MainAgent{}, errors.New("unknown agent")
		}
		return exploreAgent, nil
	})

	if got := svc.MainAgent(); got.Name != "explore" || !slices.Equal(got.Tools, exploreAgent.Tools) {
		t.Fatalf("MainAgent = %+v, want explore recovered with its tool policy", got)
	}
	if err := svc.SelectMainAgent(exploreAgent); err != nil {
		t.Fatal(err)
	}
	conv := []sdk.Message{}
	svc.injectMainAgentChange(newReminderAgentCtx(&conv, 1, 0))
	if len(conv) != 0 {
		t.Fatal("a client re-sending the agent a resumed conversation runs as must append nothing")
	}
}

func TestTheLaunchAgentIsTheStartingAgent(t *testing.T) {
	svc := newMainAgentService(conversation.NewInMemoryConversationRepository(nil, nil))
	svc.SetMainAgentResolver("explore", func(string) (agentdomain.MainAgent, error) { return exploreAgent, nil })
	if got := svc.MainAgent(); got.Name != "explore" {
		t.Fatalf("MainAgent = %q, want the agent the worker was launched as", got.Name)
	}
	if err := svc.SelectMainAgent(exploreAgent); err != nil {
		t.Fatal(err)
	}
	conv := []sdk.Message{}
	svc.injectMainAgentChange(newReminderAgentCtx(&conv, 1, 0))
	if len(conv) != 0 {
		t.Fatal("the launch agent's body is already message[0], so selecting it appends nothing")
	}
}
