package services

import (
	"github.com/inference-gateway/cli/internal/domain"
)

// StreamingChatService implements ChatService by delegating to AgentService
type StreamingChatService struct {
	agentService domain.AgentService
}

// NewStreamingChatService creates a new streaming chat service that delegates to AgentService
func NewStreamingChatService(agentService domain.AgentService) *StreamingChatService {
	return &StreamingChatService{
		agentService: agentService,
	}
}

// CancelRequest cancels an active request by delegating to AgentService
func (s *StreamingChatService) CancelRequest(requestID string) error {
	return s.agentService.CancelRequest(requestID)
}

// GetMetrics returns metrics for a completed request by delegating to AgentService
func (s *StreamingChatService) GetMetrics(requestID string) *domain.ChatMetrics {
	return s.agentService.GetMetrics(requestID)
}
