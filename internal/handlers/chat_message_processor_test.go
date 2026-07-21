package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	mocks "github.com/inference-gateway/cli/tests/mocks/domain"

	tea "charm.land/bubbletea/v2"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	storage "github.com/inference-gateway/cli/internal/infra/storage"
	models "github.com/inference-gateway/cli/internal/models"
	services "github.com/inference-gateway/cli/internal/services"
	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
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
				nil, // githubIssueService
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

func TestChatMessageProcessor_expandIssueReferences(t *testing.T) {
	issue123 := &domain.GitHubIssue{
		Number: 123,
		Title:  "Add login",
		Body:   "Implement auth.",
		State:  "OPEN",
		URL:    "https://github.com/o/r/issues/123",
		Comments: []domain.GitHubIssueComment{
			{Author: "alice", Body: "first comment", CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
		},
	}

	tests := []struct {
		name      string
		content   string
		setup     func(*mocks.FakeGitHubIssueService)
		assertOut func(t *testing.T, out string)
		fetchCt   int
	}{
		{
			name:    "no issue refs - passthrough",
			content: "just text",
			setup:   func(s *mocks.FakeGitHubIssueService) {},
			assertOut: func(t *testing.T, out string) {
				assert.Equal(t, "just text", out)
			},
		},
		{
			name:    "single issue ref expanded with title and body",
			content: "fix #123 please",
			setup: func(s *mocks.FakeGitHubIssueService) {
				s.GetIssueReturns(issue123, nil)
			},
			assertOut: func(t *testing.T, out string) {
				assert.Contains(t, out, "GitHub Issue #123 (OPEN): Add login")
				assert.Contains(t, out, "Implement auth.")
				assert.Contains(t, out, "[@alice, 2024-01-01]: first comment")
				assert.True(t, strings.HasPrefix(out, "fix "), "leading content preserved")
				assert.True(t, strings.HasSuffix(out, " please"), "trailing content preserved")
			},
			fetchCt: 1,
		},
		{
			name:    "duplicate refs share one fetch",
			content: "look at #1 and #1 again",
			setup: func(s *mocks.FakeGitHubIssueService) {
				s.GetIssueReturns(&domain.GitHubIssue{Number: 1, Title: "t", State: "OPEN"}, nil)
			},
			assertOut: func(t *testing.T, out string) {
				assert.Equal(t, 2, strings.Count(out, "GitHub Issue #1"))
			},
			fetchCt: 1,
		},
		{
			name:    "fetch failure leaves raw token",
			content: "ref #999",
			setup: func(s *mocks.FakeGitHubIssueService) {
				s.GetIssueReturns(nil, errors.New("not found"))
			},
			assertOut: func(t *testing.T, out string) {
				assert.Equal(t, "ref #999", out)
			},
			fetchCt: 1,
		},
		{
			name:    "no leading whitespace - not matched",
			content: "phone-555#1234",
			setup: func(s *mocks.FakeGitHubIssueService) {
				s.GetIssueReturns(&domain.GitHubIssue{Number: 1234}, nil)
			},
			assertOut: func(t *testing.T, out string) {
				assert.Equal(t, "phone-555#1234", out)
			},
			fetchCt: 0,
		},
		{
			name:    "start-of-string ref is matched",
			content: "#42 is the answer",
			setup: func(s *mocks.FakeGitHubIssueService) {
				s.GetIssueReturns(&domain.GitHubIssue{Number: 42, Title: "Life", State: "OPEN"}, nil)
			},
			assertOut: func(t *testing.T, out string) {
				assert.Contains(t, out, "GitHub Issue #42 (OPEN): Life")
				assert.True(t, strings.HasSuffix(out, " is the answer"))
			},
			fetchCt: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeGH := &mocks.FakeGitHubIssueService{}
			if tt.setup != nil {
				tt.setup(fakeGH)
			}
			handler := &ChatHandler{githubIssueService: fakeGH}
			processor := NewChatMessageProcessor(handler)
			out := processor.expandIssueReferences(context.Background(), tt.content)
			tt.assertOut(t, out)
			assert.Equal(t, tt.fetchCt, fakeGH.GetIssueCallCount(), "GetIssue call count mismatch")
		})
	}
}

func TestChatMessageProcessor_expandIssueReferences_NilService(t *testing.T) {
	handler := &ChatHandler{}
	processor := NewChatMessageProcessor(handler)
	out := processor.expandIssueReferences(context.Background(), "look at #1")
	assert.Equal(t, "look at #1", out)
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
	models.SetGatewayContextWindows(map[string]int{"moonshot/moonshot-v1-8k": 8192})
	t.Cleanup(func() { models.SetGatewayContextWindows(nil) })
	mgr, repo, cleanup := newChatRolloverFixture(t)
	defer cleanup()

	require.NoError(t, repo.StartNewConversation("Initial"))
	require.NoError(t, repo.AddMessage(domain.ConversationEntry{
		Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("hi")},
		Time:    time.Now(),
	}))

	require.NoError(t, repo.AddTokenUsage("moonshot/moonshot-v1-8k", 25000, 100, 25100))

	mockModel := &mocks.FakeModelService{}
	mockModel.GetCurrentModelReturns("moonshot/moonshot-v1-8k")
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
	skills := stubSkillsService{names: map[string]struct{}{"maintainer": {}, "ponytail": {}}}
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
		{"plugin skill", "/ponytail:ponytail do something", true},
		{"plugin skill unknown name", "/ponytail:unknown-skill hello", false},
		{"plugin skill unknown plugin", "/unknown:totally-unknown hello", false},
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
