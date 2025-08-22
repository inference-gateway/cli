package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
)

// AgentServiceImpl implements the AgentService interface with direct chat functionality
type AgentServiceImpl struct {
	client         sdk.Client
	toolService    domain.ToolService
	systemPrompt   string
	timeoutSeconds int
	maxTokens      int

	// Request tracking
	activeRequests map[string]context.CancelFunc
	requestsMux    sync.RWMutex

	// Metrics tracking
	metrics    map[string]*domain.ChatMetrics
	metricsMux sync.RWMutex
}

// NewAgentService creates a new agent service with pre-configured client
func NewAgentService(client sdk.Client, toolService domain.ToolService, systemPrompt string, timeoutSeconds int, maxTokens int) *AgentServiceImpl {
	return &AgentServiceImpl{
		client:         client,
		toolService:    toolService,
		systemPrompt:   systemPrompt,
		timeoutSeconds: timeoutSeconds,
		maxTokens:      maxTokens,
		activeRequests: make(map[string]context.CancelFunc),
		metrics:        make(map[string]*domain.ChatMetrics),
	}
}

// Run executes an agent task synchronously (for background/batch processing)
func (s *AgentServiceImpl) Run(ctx context.Context, req *domain.AgentRequest) (*domain.ChatSyncResponse, error) {
	if err := s.validateRequest(req); err != nil {
		return nil, err
	}

	messages := s.addToolsIfAvailable(req.Messages)

	logger.Info("LLM Request (Sync)",
		"request_id", req.RequestID,
		"model", req.Model,
		"messages", messages)

	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(s.timeoutSeconds)*time.Second)
	defer cancel()

	startTime := time.Now()

	response, err := s.generateContentSync(timeoutCtx, req.Model, messages)
	if err != nil {
		return nil, fmt.Errorf("failed to generate content: %w", err)
	}

	duration := time.Since(startTime)

	var content string
	var toolCalls []sdk.ChatCompletionMessageToolCall

	if len(response.Choices) > 0 {
		choice := response.Choices[0]
		content = choice.Message.Content

		if choice.Message.ToolCalls != nil {
			toolCalls = *choice.Message.ToolCalls
		}
	}

	return &domain.ChatSyncResponse{
		RequestID: req.RequestID,
		Content:   content,
		ToolCalls: toolCalls,
		Usage:     response.Usage,
		Duration:  duration,
	}, nil
}

// RunWithStream executes an agent task with streaming (for interactive chat)
func (s *AgentServiceImpl) RunWithStream(ctx context.Context, req *domain.AgentRequest) (<-chan domain.ChatEvent, error) {
	if err := s.validateRequest(req); err != nil {
		return nil, err
	}

	messages := s.addToolsIfAvailable(req.Messages)

	logger.Info("LLM Request",
		"request_id", req.RequestID,
		"model", req.Model,
		"messages", messages)

	timeoutCtx, cancel := s.setupRequest(ctx, req.RequestID)
	events := make(chan domain.ChatEvent, 100)

	go s.processStreamingRequest(timeoutCtx, cancel, req.RequestID, req.Model, messages, events)

	return events, nil
}

// CancelRequest cancels an active request
func (s *AgentServiceImpl) CancelRequest(requestID string) error {
	s.requestsMux.RLock()
	cancel, exists := s.activeRequests[requestID]
	s.requestsMux.RUnlock()

	if !exists {
		return fmt.Errorf("request %s not found or already completed", requestID)
	}

	cancel()
	return nil
}

// GetMetrics returns metrics for a completed request
func (s *AgentServiceImpl) GetMetrics(requestID string) *domain.ChatMetrics {
	s.metricsMux.RLock()
	defer s.metricsMux.RUnlock()

	if metrics, exists := s.metrics[requestID]; exists {
		return &domain.ChatMetrics{
			Duration: metrics.Duration,
			Usage:    metrics.Usage,
		}
	}
	return nil
}
