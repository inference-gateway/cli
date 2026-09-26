package directexec

import (
	"sync"

	tea "charm.land/bubbletea/v2"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

// stateStore is the narrow slice of the app state manager the direct
// executor needs: the current agent mode, chat-session lookup, and
// tool-execution session bookkeeping. *statemanager.Store satisfies it.
type stateStore interface {
	agentdomain.AgentModeState
	tui.ChatSessionState
	tui.ToolExecutionState
}

// Service is the concrete DirectExecutionService.
type Service struct {
	conversationRepo       convdomain.ConversationRepository
	toolService            agentdomain.ToolService
	stateManager           stateStore
	backgroundShellService scheddomain.BackgroundShellService
	listener               tui.ChatEventListener

	bashDetachChan     chan<- struct{}
	bashDetachChanMu   sync.RWMutex
	bashEventChannel   <-chan tea.Msg
	bashEventChannelMu sync.RWMutex
	toolEventChannel   <-chan tea.Msg
	toolEventChannelMu sync.RWMutex
}

// Options bundles the dependencies needed to construct a Service.
type Options struct {
	ConversationRepo       convdomain.ConversationRepository
	ToolService            agentdomain.ToolService
	StateStore             stateStore
	BackgroundShellService scheddomain.BackgroundShellService
	Listener               tui.ChatEventListener
}

// NewService creates a new DirectExecutionService.
func NewService(opts Options) *Service {
	return &Service{
		conversationRepo:       opts.ConversationRepo,
		toolService:            opts.ToolService,
		stateManager:           opts.StateStore,
		backgroundShellService: opts.BackgroundShellService,
		listener:               opts.Listener,
	}
}

// SetBashDetachChan satisfies agentdomain.BashDetachChannelHolder.
func (s *Service) SetBashDetachChan(ch chan<- struct{}) {
	s.bashDetachChanMu.Lock()
	defer s.bashDetachChanMu.Unlock()
	s.bashDetachChan = ch
}

// GetBashDetachChan satisfies agentdomain.BashDetachChannelHolder.
func (s *Service) GetBashDetachChan() chan<- struct{} {
	s.bashDetachChanMu.RLock()
	defer s.bashDetachChanMu.RUnlock()
	return s.bashDetachChan
}

// ClearBashDetachChan satisfies agentdomain.BashDetachChannelHolder.
func (s *Service) ClearBashDetachChan() {
	s.bashDetachChanMu.Lock()
	defer s.bashDetachChanMu.Unlock()
	s.bashDetachChan = nil
}

// PendingBashChannel returns the in-flight bash event channel (or nil if no
// bash command is currently executing). The ToolExecutionCoordinator reads
// this to know which channel to keep pumping in handleBashOutputChunk /
// handleToolExecutionProgress.
func (s *Service) PendingBashChannel() <-chan tea.Msg {
	s.bashEventChannelMu.RLock()
	defer s.bashEventChannelMu.RUnlock()
	return s.bashEventChannel
}

// PendingToolChannel returns the in-flight tool event channel (or nil if no
// !!tool command is currently executing).
func (s *Service) PendingToolChannel() <-chan tea.Msg {
	s.toolEventChannelMu.RLock()
	defer s.toolEventChannelMu.RUnlock()
	return s.toolEventChannel
}

// setBashEventChannel atomically assigns the bash event channel.
func (s *Service) setBashEventChannel(ch <-chan tea.Msg) {
	s.bashEventChannelMu.Lock()
	defer s.bashEventChannelMu.Unlock()
	s.bashEventChannel = ch
}

// setToolEventChannel atomically assigns the tool event channel.
func (s *Service) setToolEventChannel(ch <-chan tea.Msg) {
	s.toolEventChannelMu.Lock()
	defer s.toolEventChannelMu.Unlock()
	s.toolEventChannel = ch
}
