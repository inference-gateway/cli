package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"
	tuimocks "github.com/inference-gateway/cli/tests/mocks/tui"

	tea "charm.land/bubbletea/v2"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	conversation "github.com/inference-gateway/cli/internal/conversation"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	models "github.com/inference-gateway/cli/internal/platform/models"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
	shortcuts "github.com/inference-gateway/cli/internal/presentation/shortcuts"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	statemanager "github.com/inference-gateway/cli/internal/presentation/tui/statemanager"
	toolformatter "github.com/inference-gateway/cli/internal/presentation/tui/toolformatter"
)

func TestChatMessageProcessor_handleUserInput(t *testing.T) {
	tests := []struct {
		name        string
		input       agentdomain.UserInputEvent
		setupMocks  func(*agentdomainmocks.FakeFileService)
		expectError bool
	}{
		{
			name: "Regular message",
			input: agentdomain.UserInputEvent{
				Content: "Hello world",
			},
			setupMocks: func(fileService *agentdomainmocks.FakeFileService) {
			},
			expectError: false,
		},
		{
			name: "Slash command",
			input: agentdomain.UserInputEvent{
				Content: "/help",
			},
			setupMocks: func(fileService *agentdomainmocks.FakeFileService) {
			},
			expectError: false,
		},
		{
			name: "Tool command",
			input: agentdomain.UserInputEvent{
				Content: "!!Read(file_path=\"test.txt\")",
			},
			setupMocks: func(fileService *agentdomainmocks.FakeFileService) {
			},
			expectError: false,
		},
		{
			name: "Bash command",
			input: agentdomain.UserInputEvent{
				Content: "!ls -la",
			},
			setupMocks: func(fileService *agentdomainmocks.FakeFileService) {
			},
			expectError: false,
		},
		{
			name: "Message with file reference",
			input: agentdomain.UserInputEvent{
				Content: "Please check @test.go",
			},
			setupMocks: func(fileService *agentdomainmocks.FakeFileService) {
				fileService.ValidateFileReturns(nil)
				fileService.ReadFileReturns("package main\nfunc main() {}", nil)
			},
			expectError: false,
		},
		{
			name: "Message with invalid file reference",
			input: agentdomain.UserInputEvent{
				Content: "Check @nonexistent.go",
			},
			setupMocks: func(fileService *agentdomainmocks.FakeFileService) {
				fileService.ValidateFileReturns(errors.New("file not found"))
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockFile := &agentdomainmocks.FakeFileService{}
			mockAgent := &agentdomainmocks.FakeAgentService{}
			mockModel := &convmocks.FakeModelService{}
			mockTool := &agentdomainmocks.FakeToolService{}

			if tt.setupMocks != nil {
				tt.setupMocks(mockFile)
			}

			conversationRepo := conversation.NewInMemoryConversationRepository(nil, nil)
			shortcutRegistry := shortcuts.NewRegistry()
			stateManager := statemanager.NewStateManager(false)
			messageQueue := conversation.NewMessageQueueService()

			fakeDirect := &tuimocks.FakeDirectExecutionService{}
			fakeDirect.HandleBashCommandReturns(func() tea.Msg { return nil })
			fakeDirect.HandleToolCommandReturns(func() tea.Msg { return nil })

			fakeRunner := &tuimocks.FakeChatCompletionRunner{}
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
		setupMocks     func(*agentdomainmocks.FakeFileService)
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
			setupMocks: func(fileService *agentdomainmocks.FakeFileService) {
				fileService.ValidateFileReturns(nil)
				fileService.ReadFileReturns("package main", nil)
			},
			expectedOutput: "Check File: test.go\n```test.go\npackage main\n```\n",
			expectError:    false,
		},
		{
			name:    "Multiple file references",
			content: "Check @file1.go and @file2.go",
			setupMocks: func(fileService *agentdomainmocks.FakeFileService) {
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
			setupMocks: func(fileService *agentdomainmocks.FakeFileService) {
				fileService.ValidateFileReturns(nil)
				fileService.ReadFileReturns("# Title\n\n## Summary\nThis is the summary\n\n## Details\nMore details", nil)
			},
			expectedOutput: "Check File: README.md\n```README.md\n## Summary\nThis is the summary\n\n```\n",
			expectError:    false,
		},
		{
			name:    "Invalid file reference",
			content: "Check @invalid.go",
			setupMocks: func(fileService *agentdomainmocks.FakeFileService) {
				fileService.ValidateFileReturns(errors.New("file not found"))
			},
			expectedOutput: "Check @invalid.go",
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockFile := &agentdomainmocks.FakeFileService{}

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
				assert.Equal(t, tt.expectedOutput, result)
			}
		})
	}
}

// Images referenced with @ are expanded to a path-only "[Image: <path>]" token -
// the bytes are never inlined into the message, so non-vision models still learn
// which file was selected and tools like ImageEdit can act on the path.
func TestChatMessageProcessor_expandFileReferences_ImagePathOnly(t *testing.T) {
	mockFile := &agentdomainmocks.FakeFileService{}
	mockFile.ValidateFileReturns(nil)
	mockImage := &agentdomainmocks.FakeImageService{}
	mockImage.IsImageFileReturns(true)

	processor := NewChatMessageProcessor(&ChatHandler{
		fileService:  mockFile,
		imageService: mockImage,
	})

	result, err := processor.expandFileReferences("Edit @.infer/tmp/cat.png please")
	assert.NoError(t, err)
	assert.Equal(t, "Edit [Image file: .infer/tmp/cat.png - pass this path directly to image tools (e.g. ImageEdit), or to ImageDecode for a text description; it cannot be opened with Read] please", result)
	assert.Zero(t, mockFile.ReadFileCallCount(), "image bytes must not be read/inlined")
}

func TestChatMessageProcessor_expandIssueReferences(t *testing.T) {
	issue123 := &agentdomain.GitHubIssue{
		Number: 123,
		Title:  "Add login",
		Body:   "Implement auth.",
		State:  "OPEN",
		URL:    "https://github.com/o/r/issues/123",
		Comments: []agentdomain.GitHubIssueComment{
			{Author: "alice", Body: "first comment", CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
		},
	}

	tests := []struct {
		name      string
		content   string
		setup     func(*agentdomainmocks.FakeGitHubIssueService)
		assertOut func(t *testing.T, out string)
		fetchCt   int
	}{
		{
			name:    "no issue refs - passthrough",
			content: "just text",
			setup:   func(s *agentdomainmocks.FakeGitHubIssueService) {},
			assertOut: func(t *testing.T, out string) {
				assert.Equal(t, "just text", out)
			},
		},
		{
			name:    "single issue ref expanded with title and body",
			content: "fix #123 please",
			setup: func(s *agentdomainmocks.FakeGitHubIssueService) {
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
			setup: func(s *agentdomainmocks.FakeGitHubIssueService) {
				s.GetIssueReturns(&agentdomain.GitHubIssue{Number: 1, Title: "t", State: "OPEN"}, nil)
			},
			assertOut: func(t *testing.T, out string) {
				assert.Equal(t, 2, strings.Count(out, "GitHub Issue #1"))
			},
			fetchCt: 1,
		},
		{
			name:    "fetch failure leaves raw token",
			content: "ref #999",
			setup: func(s *agentdomainmocks.FakeGitHubIssueService) {
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
			setup: func(s *agentdomainmocks.FakeGitHubIssueService) {
				s.GetIssueReturns(&agentdomain.GitHubIssue{Number: 1234}, nil)
			},
			assertOut: func(t *testing.T, out string) {
				assert.Equal(t, "phone-555#1234", out)
			},
			fetchCt: 0,
		},
		{
			name:    "start-of-string ref is matched",
			content: "#42 is the answer",
			setup: func(s *agentdomainmocks.FakeGitHubIssueService) {
				s.GetIssueReturns(&agentdomain.GitHubIssue{Number: 42, Title: "Life", State: "OPEN"}, nil)
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
			fakeGH := &agentdomainmocks.FakeGitHubIssueService{}
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
func newChatRolloverFixture(t *testing.T) (*conversation.SessionRolloverManager, *conversation.PersistentConversationRepository, func()) {
	t.Helper()

	storageBackend, err := storage.NewSQLiteStorage(storage.SQLiteConfig{Path: ":memory:"})
	require.NoError(t, err)
	repo := conversation.NewPersistentConversationRepository(&toolformatter.ToolFormatterService{}, nil, storageBackend)

	cfg := &config.Config{}
	cfg.Compact.Enabled = true
	cfg.Compact.AutoAt = 80
	cfg.Compact.RolloverOnIdleMinutes = 0
	cfg.Compact.KeepFirstMessages = 2

	mgr := conversation.NewSessionRolloverManager(
		cfg,
		fakeRolloverOptimizer{},
		repo,
		conversation.NewTokenizerService(conversation.DefaultTokenizerConfig()),
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
			conversationRepo := conversation.NewInMemoryConversationRepository(nil, nil)

			for i := 0; i < tt.existingMessages; i++ {
				entry := convdomain.ConversationEntry{
					Message: sdk.Message{
						Role:    sdk.User,
						Content: sdk.NewMessageContent("test message"),
					},
				}
				_ = conversationRepo.AddMessage(entry)
			}

			mockAgent := &agentdomainmocks.FakeAgentService{}
			mockModel := &convmocks.FakeModelService{}
			stateManager := statemanager.NewStateManager(false)

			handler := &ChatHandler{
				agentService:     mockAgent,
				conversationRepo: conversationRepo,
				modelService:     mockModel,
				stateManager:     stateManager,
				messageQueue:     conversation.NewMessageQueueService(),
				completionRunner: &tuimocks.FakeChatCompletionRunner{},
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
	require.NoError(t, repo.AddMessage(convdomain.ConversationEntry{
		Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("hi")},
		Time:    time.Now(),
	}))

	require.NoError(t, repo.AddTokenUsage("moonshot/moonshot-v1-8k", 25000, 100, 25100, 0, 0))

	mockModel := &convmocks.FakeModelService{}
	mockModel.GetCurrentModelReturns("moonshot/moonshot-v1-8k")
	stateManager := statemanager.NewStateManager(false)
	fakeRunner := &tuimocks.FakeChatCompletionRunner{}
	fakeRunner.StartReturns(func() tea.Msg { return nil })

	handler := &ChatHandler{
		conversationRepo:       repo,
		sessionRolloverManager: mgr,
		modelService:           mockModel,
		stateManager:           stateManager,
		messageQueue:           conversation.NewMessageQueueService(),
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
		case tui.SetStatusEvent:
			if m.Message == "Compacting conversation..." && m.Spinner {
				sawCompactingStatus = true
			}
		case tui.RolloverCompletedEvent:
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
	conversationRepo := conversation.NewInMemoryConversationRepository(nil, nil)
	stateManager := statemanager.NewStateManager(false)
	fakeRunner := &tuimocks.FakeChatCompletionRunner{}
	fakeRunner.StartReturns(func() tea.Msg { return nil })

	handler := &ChatHandler{
		conversationRepo:       conversationRepo,
		sessionRolloverManager: nil,
		modelService:           &convmocks.FakeModelService{},
		stateManager:           stateManager,
		messageQueue:           conversation.NewMessageQueueService(),
		completionRunner:       fakeRunner,
	}
	processor := NewChatMessageProcessor(handler)

	cmd := processor.processChatMessage("hello", nil)
	require.NotNil(t, cmd)

	batch, ok := cmd().(tea.BatchMsg)
	require.True(t, ok)

	for _, sub := range batch {
		switch m := sub().(type) {
		case tui.SetStatusEvent:
			assert.NotEqual(t, "Compacting conversation...", m.Message,
				"nil rolloverManager must not produce a Compacting status")
		case tui.RolloverCompletedEvent:
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
	conversationRepo := conversation.NewInMemoryConversationRepository(nil, nil)
	stateManager := statemanager.NewStateManager(false)
	fakeRunner := &tuimocks.FakeChatCompletionRunner{}
	fakeRunner.StartReturns(func() tea.Msg { return nil })

	handler := &ChatHandler{
		conversationRepo: conversationRepo,
		modelService:     &convmocks.FakeModelService{},
		stateManager:     stateManager,
		messageQueue:     conversation.NewMessageQueueService(),
		completionRunner: fakeRunner,
	}
	handler.messageProcessor = NewChatMessageProcessor(handler)

	deferred := sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("hello after rollover")}
	cmd := handler.HandleRolloverCompletedEvent(tui.RolloverCompletedEvent{Message: deferred})
	require.NotNil(t, cmd)

	require.Equal(t, 1, conversationRepo.GetMessageCount(),
		"HandleRolloverCompletedEvent must AddMessage to resume the deferred user turn")
	assert.True(t, stateManager.IsAgentBusy(),
		"HandleRolloverCompletedEvent must SetChatPending before returning")
}

// fakeSkills returns a SkillsService whose Get resolves exactly the given
// skills; every other name is unknown. Discover mirrors Get so a test that
// activates a skill sees the same set.
func fakeSkills(skills ...agentdomain.Skill) *agentdomainmocks.FakeSkillsService {
	byName := make(map[string]agentdomain.Skill, len(skills))
	for _, sk := range skills {
		byName[sk.Name] = sk
	}
	get := func(name string) (agentdomain.Skill, bool) {
		sk, ok := byName[name]
		return sk, ok
	}
	fake := &agentdomainmocks.FakeSkillsService{}
	fake.GetStub = get
	fake.DiscoverStub = func(_ context.Context, name string) (agentdomain.Skill, bool) { return get(name) }
	return fake
}

func TestChatMessageProcessor_isSkillInvocation(t *testing.T) {
	skills := fakeSkills(agentdomain.Skill{Name: "maintainer"}, agentdomain.Skill{Name: "ponytail"})
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

// catalogSkills reports rust as a not-yet-installed catalog skill (empty Path)
// and maintainer as an installed local one.
func catalogSkills() *agentdomainmocks.FakeSkillsService {
	return fakeSkills(
		agentdomain.Skill{Name: "rust", Scope: agentdomain.SkillScopeCatalog},
		agentdomain.Skill{Name: "maintainer", Path: "/abs/.infer/skills/maintainer/SKILL.md"},
	)
}

func TestChatMessageProcessor_pendingCatalogSkills(t *testing.T) {
	p := NewChatMessageProcessor(&ChatHandler{skillsService: catalogSkills()})

	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{"catalog skill needs install", "/rust help me", []string{"rust"}},
		{"installed skill needs nothing", "/maintainer help me", nil},
		{"unknown token ignored", "/nope help me", nil},
		{"plain message ignored", "just talking", nil},
		{"deduped", "/rust and /rust again", []string{"rust"}},
		{"mid-text token counts", "please /rust this", []string{"rust"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, p.pendingCatalogSkills(tt.content))
		})
	}
}

func TestChatMessageProcessor_confirmCatalogInstall(t *testing.T) {
	p := NewChatMessageProcessor(&ChatHandler{skillsService: catalogSkills()})

	require.Nil(t, p.confirmCatalogInstall(agentdomain.UserInputEvent{Content: "/maintainer go"}),
		"an installed skill must not prompt")
	require.NotNil(t, p.confirmCatalogInstall(agentdomain.UserInputEvent{Content: "/rust go"}),
		"a catalog skill must prompt before downloading")

	// A declined skill is never re-prompted, so re-submitting the same input
	// cannot loop.
	p.declinedSkills["rust"] = true
	require.Nil(t, p.confirmCatalogInstall(agentdomain.UserInputEvent{Content: "/rust go"}))
}

func TestApprovedInstall(t *testing.T) {
	require.True(t, approvedInstall([]agentdomain.UserQuestionAnswer{{SelectedLabels: []string{"Install"}}}))
	require.False(t, approvedInstall([]agentdomain.UserQuestionAnswer{{SelectedLabels: []string{"Skip"}}}))
	require.False(t, approvedInstall(nil))
}
