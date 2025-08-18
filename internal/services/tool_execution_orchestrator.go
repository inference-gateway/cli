package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/logger"
	"github.com/inference-gateway/cli/internal/ui/shared"
	sdk "github.com/inference-gateway/sdk"
)

// ToolExecutionOrchestrator manages the complete tool execution flow
// and prevents the "stuck" state by providing clear state transitions
type ToolExecutionOrchestrator struct {
	stateManager     *StateManager
	debugService     *DebugService
	toolService      domain.ToolService
	conversationRepo domain.ConversationRepository
	configService    domain.ConfigService

	// Execution tracking
	currentExecution *ToolExecutionContext
	mutex            sync.RWMutex
}

// ToolExecutionContext tracks the context of a tool execution session
type ToolExecutionContext struct {
	SessionID    string
	RequestID    string
	ToolCalls    []sdk.ChatCompletionMessageToolCall
	CurrentIndex int
	StartTime    time.Time
	Status       ToolExecutionStatus
	Results      []*domain.ToolExecutionResult
	EventChannel chan tea.Msg
}

// ToolExecutionStatus represents the current execution status
type ToolExecutionStatus int

const (
	ToolExecutionStatusReady ToolExecutionStatus = iota
	ToolExecutionStatusProcessing
	ToolExecutionStatusWaitingApproval
	ToolExecutionStatusExecuting
	ToolExecutionStatusCompleting
	ToolExecutionStatusCompleted
	ToolExecutionStatusFailed
	ToolExecutionStatusCancelled
)

func (t ToolExecutionStatus) String() string {
	switch t {
	case ToolExecutionStatusReady:
		return "Ready"
	case ToolExecutionStatusProcessing:
		return "Processing"
	case ToolExecutionStatusWaitingApproval:
		return "WaitingApproval"
	case ToolExecutionStatusExecuting:
		return "Executing"
	case ToolExecutionStatusCompleting:
		return "Completing"
	case ToolExecutionStatusCompleted:
		return "Completed"
	case ToolExecutionStatusFailed:
		return "Failed"
	case ToolExecutionStatusCancelled:
		return "Cancelled"
	default:
		return "Unknown"
	}
}

// ToolExecutionEvents for communication with the UI

// ToolExecutionStartedMsg indicates tool execution has started
type ToolExecutionStartedMsg struct {
	SessionID  string
	TotalTools int
}

// ToolExecutionProgressMsg indicates progress in tool execution
type ToolExecutionProgressMsg struct {
	SessionID        string
	CurrentTool      int
	TotalTools       int
	ToolName         string
	Status           string
	RequiresApproval bool
}

// ToolExecutionCompletedMsg indicates tool execution is complete
type ToolExecutionCompletedMsg struct {
	SessionID     string
	TotalExecuted int
	SuccessCount  int
	FailureCount  int
	Results       []*domain.ToolExecutionResult
}

// ToolApprovalRequestMsg requests approval for a specific tool
type ToolApprovalRequestMsg struct {
	SessionID  string
	ToolCall   sdk.ChatCompletionMessageToolCall
	ToolIndex  int
	TotalTools int
}

// ToolApprovalResponseMsg provides the approval response
type ToolApprovalResponseMsg struct {
	SessionID string
	Approved  bool
	ToolIndex int
}

// NewToolExecutionOrchestrator creates a new tool execution orchestrator
func NewToolExecutionOrchestrator(
	stateManager *StateManager,
	debugService *DebugService,
	toolService domain.ToolService,
	conversationRepo domain.ConversationRepository,
	configService domain.ConfigService,
) *ToolExecutionOrchestrator {
	return &ToolExecutionOrchestrator{
		stateManager:     stateManager,
		debugService:     debugService,
		toolService:      toolService,
		conversationRepo: conversationRepo,
		configService:    configService,
	}
}

// StartToolExecution initiates a new tool execution session
func (teo *ToolExecutionOrchestrator) StartToolExecution(
	requestID string,
	toolCalls []sdk.ChatCompletionMessageToolCall,
) (string, tea.Cmd) {
	teo.mutex.Lock()
	defer teo.mutex.Unlock()

	sessionID := generateSessionID()

	teo.currentExecution = &ToolExecutionContext{
		SessionID:    sessionID,
		RequestID:    requestID,
		ToolCalls:    toolCalls,
		CurrentIndex: 0,
		StartTime:    time.Now(),
		Status:       ToolExecutionStatusReady,
		Results:      make([]*domain.ToolExecutionResult, len(toolCalls)),
		EventChannel: make(chan tea.Msg, 100),
	}

	// Update state manager
	_ = teo.stateManager.StartToolExecution(toolCalls)

	// Log debug event
	if teo.debugService != nil {
		teo.debugService.LogToolExecution("session", "started", map[string]interface{}{
			"session_id": sessionID,
			"request_id": requestID,
			"tool_count": len(toolCalls),
		})
	}

	// Start processing
	return sessionID, tea.Batch(
		func() tea.Msg {
			return ToolExecutionStartedMsg{
				SessionID:  sessionID,
				TotalTools: len(toolCalls),
			}
		},
		teo.processNextTool(),
	)
}

// processNextTool processes the next tool in the queue
func (teo *ToolExecutionOrchestrator) processNextTool() tea.Cmd {
	return func() tea.Msg {
		teo.mutex.RLock()
		execution := teo.currentExecution
		teo.mutex.RUnlock()

		if execution == nil {
			logger.Error("No active tool execution context")
			return shared.ShowErrorMsg{
				Error:  "Internal error: no active tool execution context",
				Sticky: false,
			}
		}

		// Check if we've completed all tools
		if execution.CurrentIndex >= len(execution.ToolCalls) {
			return teo.completeExecution()()
		}

		currentTool := execution.ToolCalls[execution.CurrentIndex]

		// Check if approval is required
		approvalRequired := teo.isApprovalRequired(currentTool.Function.Name)

		// Update status
		teo.mutex.Lock()
		if approvalRequired {
			execution.Status = ToolExecutionStatusWaitingApproval
		} else {
			execution.Status = ToolExecutionStatusProcessing
		}
		teo.mutex.Unlock()

		// Log progress
		if teo.debugService != nil {
			teo.debugService.LogToolExecution(currentTool.Function.Name, "processing", map[string]interface{}{
				"session_id":        execution.SessionID,
				"tool_index":        execution.CurrentIndex,
				"total_tools":       len(execution.ToolCalls),
				"approval_required": approvalRequired,
			})
		}

		// Send progress update
		progressMsg := ToolExecutionProgressMsg{
			SessionID:        execution.SessionID,
			CurrentTool:      execution.CurrentIndex + 1,
			TotalTools:       len(execution.ToolCalls),
			ToolName:         currentTool.Function.Name,
			Status:           execution.Status.String(),
			RequiresApproval: approvalRequired,
		}

		if approvalRequired {
			// Request approval
			return tea.Batch(
				func() tea.Msg { return progressMsg },
				func() tea.Msg {
					return ToolApprovalRequestMsg{
						SessionID:  execution.SessionID,
						ToolCall:   currentTool,
						ToolIndex:  execution.CurrentIndex,
						TotalTools: len(execution.ToolCalls),
					}
				},
			)()
		} else {
			// Execute immediately
			return tea.Batch(
				func() tea.Msg { return progressMsg },
				teo.executeTool(execution.CurrentIndex),
			)()
		}
	}
}

// HandleApprovalResponse handles the response to a tool approval request
func (teo *ToolExecutionOrchestrator) HandleApprovalResponse(approved bool, toolIndex int) tea.Cmd {
	return func() tea.Msg {
		teo.mutex.RLock()
		execution := teo.currentExecution
		teo.mutex.RUnlock()

		if execution == nil || execution.CurrentIndex != toolIndex {
			return shared.ShowErrorMsg{
				Error:  "Invalid approval response: no matching tool execution",
				Sticky: false,
			}
		}

		currentTool := execution.ToolCalls[execution.CurrentIndex]

		// Log approval decision
		if teo.debugService != nil {
			teo.debugService.LogToolExecution(currentTool.Function.Name, "approval_response", map[string]interface{}{
				"session_id": execution.SessionID,
				"tool_index": toolIndex,
				"approved":   approved,
			})
		}

		if approved {
			// Execute the tool
			teo.mutex.Lock()
			execution.Status = ToolExecutionStatusExecuting
			teo.mutex.Unlock()

			return teo.executeTool(toolIndex)()
		} else {
			// Skip this tool and create a denial result
			result := &domain.ToolExecutionResult{
				ToolName:  currentTool.Function.Name,
				Arguments: parseToolArguments(currentTool.Function.Arguments),
				Success:   false,
				Duration:  0,
				Error:     "Denied by user",
			}

			// Store result and move to next tool
			teo.mutex.Lock()
			execution.Results[execution.CurrentIndex] = result
			execution.CurrentIndex++
			execution.Status = ToolExecutionStatusProcessing
			teo.mutex.Unlock()

			// Add denial result to conversation
			teo.addToolResultToConversation(currentTool, result)

			// Continue with next tool
			return teo.processNextTool()()
		}
	}
}

// executeTool executes a specific tool
func (teo *ToolExecutionOrchestrator) executeTool(toolIndex int) tea.Cmd {
	return func() tea.Msg {
		teo.mutex.RLock()
		execution := teo.currentExecution
		teo.mutex.RUnlock()

		if execution == nil || toolIndex >= len(execution.ToolCalls) {
			return shared.ShowErrorMsg{
				Error:  "Invalid tool execution request",
				Sticky: false,
			}
		}

		currentTool := execution.ToolCalls[toolIndex]
		startTime := time.Now()

		// Log execution start
		if teo.debugService != nil {
			teo.debugService.LogToolExecution(currentTool.Function.Name, "execution_started", map[string]interface{}{
				"session_id": execution.SessionID,
				"tool_index": toolIndex,
			})
		}

		// Parse arguments
		args := parseToolArguments(currentTool.Function.Arguments)

		// Execute the tool
		ctx := context.Background()
		result, err := teo.toolService.ExecuteTool(ctx, currentTool.Function.Name, args)

		duration := time.Since(startTime)

		// Create execution result
		var executionResult *domain.ToolExecutionResult
		if err != nil {
			executionResult = &domain.ToolExecutionResult{
				ToolName:  currentTool.Function.Name,
				Arguments: args,
				Success:   false,
				Duration:  duration,
				Error:     err.Error(),
			}
		} else {
			executionResult = result
		}

		// Log execution completion
		if teo.debugService != nil {
			teo.debugService.LogToolExecution(currentTool.Function.Name, "execution_completed", map[string]interface{}{
				"session_id": execution.SessionID,
				"tool_index": toolIndex,
				"success":    executionResult.Success,
				"duration":   duration.String(),
			})
		}

		// Store result
		teo.mutex.Lock()
		execution.Results[toolIndex] = executionResult
		execution.CurrentIndex++
		execution.Status = ToolExecutionStatusProcessing
		teo.mutex.Unlock()

		// Add result to conversation
		teo.addToolResultToConversation(currentTool, executionResult)

		// Continue with next tool
		return teo.processNextTool()()
	}
}

// completeExecution completes the tool execution session
func (teo *ToolExecutionOrchestrator) completeExecution() tea.Cmd {
	return func() tea.Msg {
		teo.mutex.Lock()
		defer teo.mutex.Unlock()

		if teo.currentExecution == nil {
			return shared.ShowErrorMsg{
				Error:  "No active execution to complete",
				Sticky: false,
			}
		}

		execution := teo.currentExecution
		execution.Status = ToolExecutionStatusCompleted

		// Calculate statistics
		successCount := 0
		failureCount := 0
		for _, result := range execution.Results {
			if result != nil {
				if result.Success {
					successCount++
				} else {
					failureCount++
				}
			}
		}

		// Log completion
		if teo.debugService != nil {
			teo.debugService.LogToolExecution("session", "completed", map[string]interface{}{
				"session_id":    execution.SessionID,
				"total_tools":   len(execution.ToolCalls),
				"success_count": successCount,
				"failure_count": failureCount,
				"duration":      time.Since(execution.StartTime).String(),
			})
		}

		// Update state manager
		teo.stateManager.EndToolExecution()

		// Clean up
		sessionID := execution.SessionID
		results := execution.Results
		teo.currentExecution = nil

		return ToolExecutionCompletedMsg{
			SessionID:     sessionID,
			TotalExecuted: len(execution.ToolCalls),
			SuccessCount:  successCount,
			FailureCount:  failureCount,
			Results:       results,
		}
	}
}

// CancelExecution cancels the current tool execution
func (teo *ToolExecutionOrchestrator) CancelExecution(reason string) tea.Cmd {
	return func() tea.Msg {
		teo.mutex.Lock()
		defer teo.mutex.Unlock()

		if teo.currentExecution == nil {
			return nil // No active execution
		}

		execution := teo.currentExecution
		execution.Status = ToolExecutionStatusCancelled

		// Log cancellation
		if teo.debugService != nil {
			teo.debugService.LogToolExecution("session", "cancelled", map[string]interface{}{
				"session_id": execution.SessionID,
				"reason":     reason,
			})
		}

		// Update state manager
		teo.stateManager.EndToolExecution()

		// Clean up
		teo.currentExecution = nil

		return shared.SetStatusMsg{
			Message: fmt.Sprintf("Tool execution cancelled: %s", reason),
			Spinner: false,
		}
	}
}

// GetExecutionStatus returns the current execution status
func (teo *ToolExecutionOrchestrator) GetExecutionStatus() (bool, *ToolExecutionContext) {
	teo.mutex.RLock()
	defer teo.mutex.RUnlock()

	if teo.currentExecution == nil {
		return false, nil
	}

	// Return a copy to prevent external modification
	copy := *teo.currentExecution
	return true, &copy
}

// Helper methods

// isApprovalRequired checks if a tool requires approval
func (teo *ToolExecutionOrchestrator) isApprovalRequired(toolName string) bool {
	if teo.configService == nil {
		return true // Default to requiring approval
	}

	// This would integrate with the actual config service
	// For now, return a simple implementation
	return teo.configService.IsApprovalRequired(toolName)
}

// addToolResultToConversation adds a tool execution result to the conversation
func (teo *ToolExecutionOrchestrator) addToolResultToConversation(
	toolCall sdk.ChatCompletionMessageToolCall,
	result *domain.ToolExecutionResult,
) {
	if teo.conversationRepo == nil {
		return
	}

	// Create tool result message
	var content string
	if result.Success {
		if result.Data != nil {
			content = fmt.Sprintf("Tool executed successfully: %v", result.Data)
		} else {
			content = "Tool executed successfully"
		}
	} else {
		content = fmt.Sprintf("Tool execution failed: %s", result.Error)
	}

	toolResultEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:       sdk.Tool,
			Content:    content,
			ToolCallId: &toolCall.Id,
		},
		Time:          time.Now(),
		ToolExecution: result,
	}

	if err := teo.conversationRepo.AddMessage(toolResultEntry); err != nil {
		logger.Error("Failed to add tool result to conversation", "error", err)
	}
}

// parseToolArguments parses tool arguments from JSON string
func parseToolArguments(arguments string) map[string]interface{} {
	// This would be implemented to parse JSON arguments
	// For now, return empty map
	return make(map[string]interface{})
}

// generateSessionID generates a unique session ID
func generateSessionID() string {
	return fmt.Sprintf("session_%d", time.Now().UnixNano())
}

// Recovery and Health Monitoring

// RecoverFromStuckState attempts to recover from a stuck tool execution state
func (teo *ToolExecutionOrchestrator) RecoverFromStuckState() tea.Cmd {
	return func() tea.Msg {
		teo.mutex.Lock()
		defer teo.mutex.Unlock()

		if teo.currentExecution == nil {
			return nil // No active execution
		}

		execution := teo.currentExecution

		// Check if execution is stuck (no progress for too long)
		now := time.Now()
		if now.Sub(execution.StartTime) > 5*time.Minute {
			logger.Warn("Detected stuck tool execution, attempting recovery",
				"session_id", execution.SessionID,
				"current_index", execution.CurrentIndex,
				"status", execution.Status.String(),
			)

			// Force completion or cancellation
			execution.Status = ToolExecutionStatusCancelled
			teo.stateManager.EndToolExecution()
			teo.currentExecution = nil

			return shared.SetStatusMsg{
				Message: "Recovered from stuck tool execution",
				Spinner: false,
			}
		}

		return nil
	}
}

// GetHealthStatus returns the health status of the tool execution orchestrator
func (teo *ToolExecutionOrchestrator) GetHealthStatus() map[string]interface{} {
	teo.mutex.RLock()
	defer teo.mutex.RUnlock()

	status := map[string]interface{}{
		"healthy":          true,
		"active_execution": false,
	}

	if teo.currentExecution != nil {
		status["active_execution"] = true
		status["session_id"] = teo.currentExecution.SessionID
		status["current_tool"] = teo.currentExecution.CurrentIndex + 1
		status["total_tools"] = len(teo.currentExecution.ToolCalls)
		status["status"] = teo.currentExecution.Status.String()
		status["duration"] = time.Since(teo.currentExecution.StartTime).String()

		// Check if stuck
		if time.Since(teo.currentExecution.StartTime) > 5*time.Minute {
			status["healthy"] = false
			status["issue"] = "execution_stuck"
		}
	}

	return status
}
