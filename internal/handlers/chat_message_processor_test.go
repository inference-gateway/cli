package handlers

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	storage "github.com/inference-gateway/cli/internal/infra/storage"
	services "github.com/inference-gateway/cli/internal/services"
	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
	mocks "github.com/inference-gateway/cli/tests/mocks/domain"
	sdk "github.com/inference-gateway/sdk"
	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"
)

func TestChatMessageProcessor_handleUserInput(t *testing.T) {
	tests := []struct {
		name        string
		input       domain.UserInputEvent
		setupMocks  func(*mocks.FakeFileService)
		expectError bool
	}{
		{
			name: "Regular message",
			input: domain.UserInputEvent{
				Content: "Hello world",
			},
			setupMocks: func(fileService *mocks.FakeFileService) {
			},
			expectError: false,
		},
		{
			name: "Slash command",
			input: domain.UserInputEvent{
				Content: "/help",
			},
			setupMocks: func(fileService *mocks.FakeFileService) {
			},
			expectError: false,
		},
		{
			name: "Tool command",
			input: domain.UserInputEvent{
				Content: "!!Read(file_path=\"test.txt\")",
			},
			setupMocks: func(fileService *mocks.FakeFileService) {
			},
			expectError: false,
		},
		{
			name: "Bash command",
			input: domain.UserInputEvent{
				Content: "!ls -la",
			},
			setupMocks: func(fileService *mocks.FakeFileService) {
			},
			expectError: false,
		},
		{
			name: "Message with file reference",
			input: domain.UserInputEvent{
				Content: "Please check @test.go",
			},
			setupMocks: func(fileService *mocks.FakeFileService) {
				fileService.ValidateFileReturns(nil)
				fileService.ReadFileReturns("package main\nfunc main() {}", nil)
			},
			expectError: false,
		},
		{
			name: "Message with invalid file reference",
			input: domain.UserInputEvent{
				Content: "Check @nonexistent.go",
			},
			setupMocks: func(fileService *mocks.FakeFileService) {
				fileService.ValidateFileReturns(errors.New("file not found"))
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockFile := &mocks.FakeFileService{}
			mockAgent := &mocks.FakeAgentService{}
			mockModel := &mocks.FakeModelService{}
			mockTool := &mocks.FakeToolService{}

			if tt.setupMocks != nil {
				tt.setupMocks(mockFile)
			}

			conversationRepo := services.NewInMemoryConversationRepository(nil, nil)
			shortcutRegistry := shortcuts.NewRegistry()
			stateManager := services.NewStateManager(false)
			messageQueue := services.NewMessageQueueService()

			fakeDirect := &mocks.FakeDirectExecutionService{}
			fakeDirect.HandleBashCommandReturns(func() tea.Msg { return nil })
			fakeDirect.HandleToolCommandReturns(func() tea.Msg { return nil })

			fakeRunner := &mocks.FakeChatCompletionRunner{}
			fakeRunner.StartReturns(func() tea.Msg { return nil })

			handler := NewChatHandler(
				mockAgent,
				conversationRepo,
				nil, // conversationOptimizer
				nil, // sessionRolloverManager
				mockModel,
				mockTool,
				mockFile,
				nil,
				nil, // skillsService
				shortcutRegistry,
				stateManager,
				messageQueue,
				nil,
				nil,
				nil,
				nil,
				config.DefaultConfig(),
				nil, // a2aTaskCoordinator
				nil, // approvalCoordinator
				fakeRunner,
				fakeDirect,
				nil, // toolCoordinator
			)

			processor := NewChatMessageProcessor(handler)

			cmd := processor.handleUserInput(tt.input)

			if tt.expectError {
				assert.NotNil(t, cmd)
			} else {
				assert.NotNil(t, cmd)
			}
		})
	}
}

func TestChatMessageProcessor_expandFileReferences(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		setupMocks     func(*mocks.FakeFileService)
		expectedOutput string
		expectError    bool
	}{
		{
			name:           "No file references",
			content:        "Hello world",
			expectedOutput: "Hello world",
			expectError:    false,
		},
		{
			name:    "Single file reference",
			content: "Check @test.go",
			setupMocks: func(fileService *mocks.FakeFileService) {
				fileService.ValidateFileReturns(nil)
				fileService.ReadFileReturns("package main", nil)
			},
			expectedOutput: "Check File: test.go\n```test.go\npackage main\n```\n",
			expectError:    false,
		},
		{
			name:    "Multiple file references",
			content: "Check @file1.go and @file2.go",
			setupMocks: func(fileService *mocks.FakeFileService) {
				fileService.ValidateFileReturns(nil)
				fileService.ReadFileReturnsOnCall(0, "content1", nil)
				fileService.ReadFileReturnsOnCall(1, "content2", nil)
			},
			expectedOutput: "Check File: file1.go\n```file1.go\ncontent1\n```\n and File: file2.go\n```file2.go\ncontent2\n```\n",
			expectError:    false,
		},
		{
			name:    "Markdown file with summary",
			content: "Check @README.md",
			setupMocks: func(fileService *mocks.FakeFileService) {
				fileService.ValidateFileReturns(nil)
				fileService.ReadFileReturns("# Title\n\n## Summary\nThis is the summary\n\n## Details\nMore details", nil)
			},
			expectedOutput: "Check File: README.md\n```README.md\n## Summary\nThis is the summary\n\n```\n",
			expectError:    false,
		},
		{
			name:    "Invalid file reference",
			content: "Check @invalid.go",
			setupMocks: func(fileService *mocks.FakeFileService) {
				fileService.ValidateFileReturns(errors.New("file not found"))
			},
			expectedOutput: "Check @invalid.go",
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockFile := &mocks.FakeFileService{}

			if tt.setupMocks != nil {
				tt.setupMocks(mockFile)
			}

			handler := &ChatHandler{
				fileService: mockFile,
			}

			processor := NewChatMessageProcessor(handler)

			result, err := processor.expandFileReferences(tt.content)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedOutput, result.content)
			}
		})
	}
}

// fakeRolloverOptimizer is a minimal ConversationOptimizer used to exercise
// the async-rollover path in chat mode. It returns a single summary message
// regardless of input so PerformRollover always has something to write into
// the new conversation.
type fakeRolloverOptimizer struct{}

func (fakeRolloverOptimizer) OptimizeMessages(_ []sdk.Message, _ string, _ bool) []sdk.Message {
	return []sdk.Message{
		{Role: sdk.Assistant, Content: sdk.NewMessageContent("--- summary ---")},
	}
}

// newChatRolloverFixture stands up a real SessionRolloverManager backed by
// in-memory SQLite and an in-memory SessionGroupStorage. Used by the
// async-rollover handler tests; cheaper than refactoring SessionRolloverManager
// to an interface just for mocking.
func newChatRolloverFixture(t *testing.T) (*services.SessionRolloverManager, *services.PersistentConversationRepository, func()) {
	t.Helper()

	storageBackend, err := storage.NewSQLiteStorage(storage.SQLiteConfig{Path: ":memory:"})
	require.NoError(t, err)
	repo := services.NewPersistentConversationRepository(&services.ToolFormatterService{}, nil, storageBackend)

	cfg := &config.Config{}
	cfg.Compact.Enabled = true
	cfg.Compact.AutoAt = 80
	cfg.Compact.RolloverOnIdleMinutes = 0
	cfg.Compact.KeepFirstMessages = 2

	mgr := services.NewSessionRolloverManager(
		cfg,
		fakeRolloverOptimizer{},
		repo,
		services.NewTokenizerService(services.DefaultTokenizerConfig()),
		storage.NewMemorySessionGroupStorage(),
	)

	cleanup := func() {
		_ = repo.Close()
		_ = storageBackend.Close()
	}
	return mgr, repo, cleanup
}

func TestChatMessageProcessor_processChatMessage(t *testing.T) {
	tests := []struct {
		name               string
		content            string
		existingMessages   int
		expectedCmdCount   int
		expectOptimization bool
	}{
		{
			name:               "Simple message",
			content:            "Hello",
			existingMessages:   5,
			expectedCmdCount:   2,
			expectOptimization: false,
		},
		{
			name:               "Message triggering optimization",
			content:            "Hello",
			existingMessages:   15,
			expectedCmdCount:   3,
			expectOptimization: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conversationRepo := services.NewInMemoryConversationRepository(nil, nil)

			for i := 0; i < tt.existingMessages; i++ {
				entry := domain.ConversationEntry{
					Message: domain.Message{
						Role:    domain.RoleUser,
						Content: sdk.NewMessageContent("test message"),
					},
				}
				_ = conversationRepo.AddMessage(entry)
			}

			mockAgent := &mocks.FakeAgentService{}
			mockModel := &mocks.FakeModelService{}
			stateManager := services.NewStateManager(false)

			handler := &ChatHandler{
				agentService:     mockAgent,
				conversationRepo: conversationRepo,
				modelService:     mockModel,
				stateManager:     stateManager,
				messageQueue:     services.NewMessageQueueService(),
				completionRunner: &mocks.FakeChatCompletionRunner{},
			}

			processor := NewChatMessageProcessor(handler)

			cmd := processor.processChatMessage(tt.content, nil)

			assert.NotNil(t, cmd)
		})
	}
}

// TestChatMessageProcessor_processChatMessage_AsyncRolloverPath verifies that
// when the rollover gate is open, processChatMessage emits a "Compacting..."
// status and dispatches a RolloverCompletedEvent asynchronously so the
// Bubble Tea Update loop stays responsive while the summary LLM call runs.
func TestChatMessageProcessor_processChatMessage_AsyncRolloverPath(t *testing.T) {
	mgr, repo, cleanup := newChatRolloverFixture(t)
	defer cleanup()

	require.NoError(t, repo.StartNewConversation("Initial"))
	require.NoError(t, repo.AddMessage(domain.ConversationEntry{
		Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("hi")},
		Time:    time.Now(),
	}))
	// LastInputTokens above the 80% threshold for the unknown-model fallback
	// (30000 * 80 / 100 = 24000) opens the rollover gate without needing a
	// large conversation in the entries-only estimator.
	require.NoError(t, repo.AddTokenUsage("unknown-tiny-model", 25000, 100, 25100))

	mockModel := &mocks.FakeModelService{}
	mockModel.GetCurrentModelReturns("unknown-tiny-model")
	stateManager := services.NewStateManager(false)
	fakeRunner := &mocks.FakeChatCompletionRunner{}
	fakeRunner.StartReturns(func() tea.Msg { return nil })

	handler := &ChatHandler{
		conversationRepo:       repo,
		sessionRolloverManager: mgr,
		modelService:           mockModel,
		stateManager:           stateManager,
		messageQueue:           services.NewMessageQueueService(),
		completionRunner:       fakeRunner,
	}
	processor := NewChatMessageProcessor(handler)

	require.False(t, stateManager.IsAgentBusy(), "pre-condition: not busy")

	cmd := processor.processChatMessage("hello world", nil)
	require.NotNil(t, cmd)

	assert.True(t, stateManager.IsAgentBusy(),
		"compactThenContinue must SetChatPending before returning so subsequent input queues")

	batch, ok := cmd().(tea.BatchMsg)
	require.True(t, ok, "expected tea.BatchMsg from async path; got %T", cmd())

	var sawCompactingStatus, sawRolloverCompleted bool
	for _, sub := range batch {
		msg := sub()
		switch m := msg.(type) {
		case domain.SetStatusEvent:
			if m.Message == "Compacting conversation..." && m.Spinner {
				sawCompactingStatus = true
			}
		case domain.RolloverCompletedEvent:
			sawRolloverCompleted = true
			assert.Equal(t, sdk.User, m.Message.Role,
				"RolloverCompletedEvent must carry the user message that was deferred")
		}
	}
	assert.True(t, sawCompactingStatus, "expected SetStatusEvent(\"Compacting conversation...\")")
	assert.True(t, sawRolloverCompleted, "expected RolloverCompletedEvent after async rollover")
}

// TestChatMessageProcessor_processChatMessage_SyncPathWhenManagerNil verifies
// that the no-rollover-manager case still produces the synchronous AddMessage +
// startChatCompletion batch with no "Compacting..." status.
func TestChatMessageProcessor_processChatMessage_SyncPathWhenManagerNil(t *testing.T) {
	conversationRepo := services.NewInMemoryConversationRepository(nil, nil)
	stateManager := services.NewStateManager(false)
	fakeRunner := &mocks.FakeChatCompletionRunner{}
	fakeRunner.StartReturns(func() tea.Msg { return nil })

	handler := &ChatHandler{
		conversationRepo:       conversationRepo,
		sessionRolloverManager: nil,
		modelService:           &mocks.FakeModelService{},
		stateManager:           stateManager,
		messageQueue:           services.NewMessageQueueService(),
		completionRunner:       fakeRunner,
	}
	processor := NewChatMessageProcessor(handler)

	cmd := processor.processChatMessage("hello", nil)
	require.NotNil(t, cmd)

	batch, ok := cmd().(tea.BatchMsg)
	require.True(t, ok)

	for _, sub := range batch {
		switch m := sub().(type) {
		case domain.SetStatusEvent:
			assert.NotEqual(t, "Compacting conversation...", m.Message,
				"nil rolloverManager must not produce a Compacting status")
		case domain.RolloverCompletedEvent:
			t.Errorf("nil rolloverManager must not dispatch RolloverCompletedEvent")
		}
	}

	assert.Equal(t, 1, conversationRepo.GetMessageCount(),
		"sync path must AddMessage immediately, not defer until after rollover")
}

// TestChatHandler_HandleRolloverCompletedEvent verifies the handler-side
// continuation: receiving the event resumes the deferred AddMessage +
// startChatCompletion flow that processChatMessage skipped while the async
// rollover was in flight.
func TestChatHandler_HandleRolloverCompletedEvent(t *testing.T) {
	conversationRepo := services.NewInMemoryConversationRepository(nil, nil)
	stateManager := services.NewStateManager(false)
	fakeRunner := &mocks.FakeChatCompletionRunner{}
	fakeRunner.StartReturns(func() tea.Msg { return nil })

	handler := &ChatHandler{
		conversationRepo: conversationRepo,
		modelService:     &mocks.FakeModelService{},
		stateManager:     stateManager,
		messageQueue:     services.NewMessageQueueService(),
		completionRunner: fakeRunner,
	}
	handler.messageProcessor = NewChatMessageProcessor(handler)

	deferred := sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("hello after rollover")}
	cmd := handler.HandleRolloverCompletedEvent(domain.RolloverCompletedEvent{Message: deferred})
	require.NotNil(t, cmd)

	require.Equal(t, 1, conversationRepo.GetMessageCount(),
		"HandleRolloverCompletedEvent must AddMessage to resume the deferred user turn")
	assert.True(t, stateManager.IsAgentBusy(),
		"HandleRolloverCompletedEvent must SetChatPending before returning")
}

type stubSkillsService struct {
	names map[string]struct{}
}

func (s stubSkillsService) Load(context.Context) error { return nil }
func (s stubSkillsService) List() []domain.Skill       { return nil }
func (s stubSkillsService) Get(name string) (domain.Skill, bool) {
	_, ok := s.names[name]
	return domain.Skill{Name: name}, ok
}
func (s stubSkillsService) Errors() []domain.SkillLoadError { return nil }

func TestChatMessageProcessor_isSkillInvocation(t *testing.T) {
	skills := stubSkillsService{names: map[string]struct{}{"maintainer": {}}}
	p := NewChatMessageProcessor(&ChatHandler{skillsService: skills})

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"known skill", "/maintainer fix issue 5", true},
		{"known skill case-insensitive", "/Maintainer go", true},
		{"unknown skill falls through to shortcut", "/clear", false},
		{"non-slash message", "use the maintainer skill", false},
		{"unknown slash token", "/totally-unknown", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, p.isSkillInvocation(tt.content))
		})
	}
}

func TestChatMessageProcessor_isSkillInvocation_NilService(t *testing.T) {
	p := NewChatMessageProcessor(&ChatHandler{})
	require.False(t, p.isSkillInvocation("/maintainer"))
}
