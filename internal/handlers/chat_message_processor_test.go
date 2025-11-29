package handlers

import (
	"errors"
	"testing"

	domain "github.com/inference-gateway/cli/internal/domain"
	services "github.com/inference-gateway/cli/internal/services"
	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
	mocks "github.com/inference-gateway/cli/tests/mocks/generated"
	sdk "github.com/inference-gateway/sdk"
	assert "github.com/stretchr/testify/assert"
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
			mockConfig := &mocks.FakeConfigService{}
			mockTool := &mocks.FakeToolService{}

			if tt.setupMocks != nil {
				tt.setupMocks(mockFile)
			}

			conversationRepo := services.NewInMemoryConversationRepository(nil)
			shortcutRegistry := shortcuts.NewRegistry()
			stateManager := services.NewStateManager(false)
			messageQueue := services.NewMessageQueueService()

			handler := NewChatHandler(
				mockAgent,
				conversationRepo,
				nil,
				mockModel,
				mockConfig,
				mockTool,
				mockFile,
				nil,
				shortcutRegistry,
				stateManager,
				messageQueue,
				nil,
				nil,
				nil,
				nil,
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
		setupMocks     func(*mocks.FakeFileService, *mocks.FakeImageService)
		expectedOutput string
		expectedImages int
		expectError    bool
	}{
		{
			name:           "No file references",
			content:        "Hello world",
			expectedOutput: "Hello world",
			expectedImages: 0,
			expectError:    false,
		},
		{
			name:    "Single file reference",
			content: "Check @test.go",
			setupMocks: func(fileService *mocks.FakeFileService, imageService *mocks.FakeImageService) {
				fileService.ValidateFileReturns(nil)
				fileService.ReadFileReturns("package main", nil)
			},
			expectedOutput: "Check File: test.go\n```test.go\npackage main\n```\n",
			expectedImages: 0,
			expectError:    false,
		},
		{
			name:    "Multiple file references",
			content: "Check @file1.go and @file2.go",
			setupMocks: func(fileService *mocks.FakeFileService, imageService *mocks.FakeImageService) {
				fileService.ValidateFileReturns(nil)
				fileService.ReadFileReturnsOnCall(0, "content1", nil)
				fileService.ReadFileReturnsOnCall(1, "content2", nil)
			},
			expectedOutput: "Check File: file1.go\n```file1.go\ncontent1\n```\n and File: file2.go\n```file2.go\ncontent2\n```\n",
			expectedImages: 0,
			expectError:    false,
		},
		{
			name:    "Markdown file with summary",
			content: "Check @README.md",
			setupMocks: func(fileService *mocks.FakeFileService, imageService *mocks.FakeImageService) {
				fileService.ValidateFileReturns(nil)
				fileService.ReadFileReturns("# Title\n\n## Summary\nThis is the summary\n\n## Details\nMore details", nil)
			},
			expectedOutput: "Check File: README.md\n```README.md\n## Summary\nThis is the summary\n\n```\n",
			expectedImages: 0,
			expectError:    false,
		},
		{
			name:    "Invalid file reference",
			content: "Check @invalid.go",
			setupMocks: func(fileService *mocks.FakeFileService, imageService *mocks.FakeImageService) {
				fileService.ValidateFileReturns(errors.New("file not found"))
			},
			expectedOutput: "Check @invalid.go",
			expectedImages: 0,
			expectError:    false,
		},
		{
			name:    "Markdown image with URL",
			content: "Look at this ![screenshot](https://example.com/image.png)",
			setupMocks: func(fileService *mocks.FakeFileService, imageService *mocks.FakeImageService) {
				imageService.IsImageURLReturns(true)
				imageService.ReadImageFromURLReturns(&domain.ImageAttachment{
					Data:     "base64data",
					MimeType: "image/png",
					Filename: "image.png",
				}, nil)
			},
			expectedOutput: "Look at this [Image: screenshot]",
			expectedImages: 1,
			expectError:    false,
		},
		{
			name:    "Markdown image with file path",
			content: "Check ![local image](./test.png)",
			setupMocks: func(fileService *mocks.FakeFileService, imageService *mocks.FakeImageService) {
				imageService.IsImageURLReturns(false)
				fileService.ValidateFileReturns(nil)
				imageService.IsImageFileReturns(true)
				imageService.ReadImageFromFileReturns(&domain.ImageAttachment{
					Data:     "base64data",
					MimeType: "image/png",
					Filename: "./test.png",
				}, nil)
			},
			expectedOutput: "Check [Image: local image]",
			expectedImages: 1,
			expectError:    false,
		},
		{
			name:    "Markdown image without alt text",
			content: "See ![](https://example.com/diagram.jpg)",
			setupMocks: func(fileService *mocks.FakeFileService, imageService *mocks.FakeImageService) {
				imageService.IsImageURLReturns(true)
				imageService.ReadImageFromURLReturns(&domain.ImageAttachment{
					Data:     "base64data",
					MimeType: "image/jpeg",
					Filename: "diagram.jpg",
				}, nil)
			},
			expectedOutput: "See [Image: https://example.com/diagram.jpg]",
			expectedImages: 1,
			expectError:    false,
		},
		{
			name:    "Mixed markdown and @ syntax",
			content: "Compare ![diagram](https://example.com/chart.png) with @code.go",
			setupMocks: func(fileService *mocks.FakeFileService, imageService *mocks.FakeImageService) {
				imageService.IsImageURLReturns(true)
				imageService.ReadImageFromURLReturns(&domain.ImageAttachment{
					Data:     "base64data",
					MimeType: "image/png",
					Filename: "chart.png",
				}, nil)
				fileService.ValidateFileReturns(nil)
				fileService.ReadFileReturns("package main", nil)
			},
			expectedOutput: "Compare [Image: diagram] with File: code.go\n```code.go\npackage main\n```\n",
			expectedImages: 1,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockFile := &mocks.FakeFileService{}
			mockImage := &mocks.FakeImageService{}

			if tt.setupMocks != nil {
				tt.setupMocks(mockFile, mockImage)
			}

			handler := &ChatHandler{
				fileService:  mockFile,
				imageService: mockImage,
			}

			processor := NewChatMessageProcessor(handler)

			result, err := processor.expandFileReferences(tt.content)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedOutput, result.content)
				assert.Equal(t, tt.expectedImages, len(result.images))
			}
		})
	}
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
			conversationRepo := services.NewInMemoryConversationRepository(nil)

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
			}

			processor := NewChatMessageProcessor(handler)

			cmd := processor.processChatMessage(tt.content, nil)

			assert.NotNil(t, cmd)
		})
	}
}
