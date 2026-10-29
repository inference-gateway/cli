package handlers

import (
	"strings"
	"testing"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"
	tuimocks "github.com/inference-gateway/cli/tests/mocks/tui"

	tea "charm.land/bubbletea/v2"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	conversation "github.com/inference-gateway/cli/internal/conversation"
	convinfra "github.com/inference-gateway/cli/internal/conversation/infrastructure"
	shortcuts "github.com/inference-gateway/cli/internal/presentation/shortcuts"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	statemanager "github.com/inference-gateway/cli/internal/presentation/tui/statemanager"
)

var (
	inProcess   = tui.SessionPlacement{}
	localDaemon = tui.SessionPlacement{Daemon: true, Local: "laptop"}
	remote      = tui.SessionPlacement{Daemon: true, Local: "laptop", Environment: "k8s-dev"}
)

// placedChat is a chat handler for a session at placement, with the fakes
// that tell where its input went.
type placedChat struct {
	handler *ChatHandler
	state   *statemanager.Store
	direct  *tuimocks.FakeDirectExecutionService
	runner  *tuimocks.FakeChatCompletionRunner
	files   *agentdomainmocks.FakeFileService
	repo    *conversation.InMemoryConversationRepository
}

func newPlacedChat(placement tui.SessionPlacement) placedChat {
	state := statemanager.NewStore(false)
	state.SetSessionPlacement(placement)
	c := placedChat{
		state:  state,
		direct: &tuimocks.FakeDirectExecutionService{},
		runner: &tuimocks.FakeChatCompletionRunner{},
		files:  &agentdomainmocks.FakeFileService{},
		repo:   conversation.NewInMemoryConversationRepository(nil, nil),
	}
	c.direct.HandleBashCommandReturns(func() tea.Msg { return nil })
	c.direct.HandleToolCommandReturns(func() tea.Msg { return nil })
	c.direct.HandleBackgroundShellRequestReturns(func() tea.Msg { return nil })
	c.runner.StartReturns(func() tea.Msg { return nil })
	registry := shortcuts.NewRegistry()
	registry.Register(shortcuts.NewDiffShortcut())
	registry.Register(shortcuts.NewCompactShortcut(c.repo))
	c.handler = NewChatHandler(
		&agentdomainmocks.FakeAgentService{}, c.repo, nil, nil, &convmocks.FakeModelService{},
		&agentdomainmocks.FakeToolService{}, c.files, nil, nil, nil, registry, state,
		convinfra.NewMessageQueueService(), nil, nil, nil, nil, config.DefaultConfig(), nil,
		c.runner, c.direct, nil, nil, nil,
	)
	return c
}

func (c placedChat) send(content string) {
	NewChatMessageProcessor(c.handler).handleUserInput(agentdomain.UserInputEvent{Content: content})
}

func (c placedChat) lastMessage(t *testing.T) string {
	t.Helper()
	entries := c.repo.GetMessages()
	if len(entries) == 0 {
		return ""
	}
	text, _ := entries[len(entries)-1].Message.Content.AsMessageContent0()
	return text
}

func statusOf(cmd tea.Cmd) string {
	if cmd == nil {
		return ""
	}
	status, _ := cmd().(tui.SetStatusEvent)
	return status.Message
}

func TestDaemonChatSendsDirectCommandsToItsWorker(t *testing.T) {
	for _, input := range []string{"!ls", `!!Read(file_path="README.md")`} {
		t.Run(input, func(t *testing.T) {
			daemon := newPlacedChat(localDaemon)
			daemon.send(input)
			if daemon.direct.HandleBashCommandCallCount()+daemon.direct.HandleToolCommandCallCount() != 0 {
				t.Error("a daemon chat ran the command in the TUI")
			}
			if daemon.runner.StartCallCount() != 1 || daemon.lastMessage(t) != input {
				t.Errorf("turns = %d with last message %q, want one turn carrying %q", daemon.runner.StartCallCount(), daemon.lastMessage(t), input)
			}

			local := newPlacedChat(inProcess)
			local.send(input)
			if local.direct.HandleBashCommandCallCount()+local.direct.HandleToolCommandCallCount() != 1 {
				t.Error("an in-process chat did not run the command itself")
			}
		})
	}
}

func TestRemoteSessionLeavesFileReferencesToItsWorker(t *testing.T) {
	tests := []struct {
		name      string
		placement tui.SessionPlacement
		reads     int
	}{
		{name: "local daemon session", placement: localDaemon, reads: 1},
		{name: "remote session", placement: remote, reads: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chat := newPlacedChat(tt.placement)
			chat.send("explain @README.md")
			if got := chat.files.ValidateFileCallCount(); got != tt.reads {
				t.Errorf("the TUI looked at this machine's files %d times, want %d", got, tt.reads)
			}
		})
	}
}

func TestPlacedShortcutsSayWhyTheyAreUnavailable(t *testing.T) {
	tests := []struct {
		name      string
		placement tui.SessionPlacement
		command   string
		reason    string
	}{
		{name: "diff on a remote session", placement: remote, command: "/diff", reason: "runs on k8s-dev"},
		{name: "compact on a daemon session", placement: localDaemon, command: "/compact", reason: "compacts the session itself"},
		{name: "diff on a local daemon session", placement: localDaemon, command: "/diff"},
		{name: "compact in process", placement: inProcess, command: "/compact"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := statusOf(newPlacedChat(tt.placement).handler.HandleCommand(tt.command))
			unavailable := strings.Contains(status, "is unavailable")
			if unavailable != (tt.reason != "") || !strings.Contains(status, tt.reason) {
				t.Errorf("status = %q, want unavailable=%v naming %q", status, tt.reason != "", tt.reason)
			}
		})
	}
}

func TestBackgroundShellIsUnavailableInADaemonChat(t *testing.T) {
	daemon := newPlacedChat(localDaemon)
	if status := statusOf(daemon.handler.HandleBackgroundShellRequest()); !strings.Contains(status, "is unavailable") {
		t.Errorf("status = %q, want the reason a daemon chat cannot detach the shell", status)
	}
	if daemon.direct.HandleBackgroundShellRequestCallCount() != 0 {
		t.Error("a daemon chat detached a shell in the TUI")
	}

	local := newPlacedChat(inProcess)
	_ = local.handler.HandleBackgroundShellRequest()
	if local.direct.HandleBackgroundShellRequestCallCount() != 1 {
		t.Error("an in-process chat did not detach its shell")
	}
}

func TestFollowRunStartsATurnOnlyWhileTheChatIsIdle(t *testing.T) {
	idle := newPlacedChat(localDaemon)
	idle.handler.Handle(tui.FollowRunEvent{})
	if idle.runner.StartCallCount() != 1 {
		t.Error("an idle chat did not follow the run another client started")
	}

	busy := newPlacedChat(localDaemon)
	_ = busy.state.StartChatSession("req-1", "m", make(chan agentdomain.ChatEvent))
	busy.handler.Handle(tui.FollowRunEvent{})
	if busy.runner.StartCallCount() != 0 {
		t.Error("a chat in a turn started a second one to follow a run")
	}
}
