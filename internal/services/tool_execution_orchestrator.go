package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
)

// ToolExecutionOrchestrator manages the complete tool execution flow
type ToolExecutionOrchestrator struct {
	stateManager     *StateManager
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

// NewToolExecutionOrchestrator creates a new tool execution orchestrator
func NewToolExecutionOrchestrator(
	stateManager *StateManager,
	toolService domain.ToolService,
	conversationRepo domain.ConversationRepository,
	configService domain.ConfigService,
) *ToolExecutionOrchestrator {
	return &ToolExecutionOrchestrator{
		stateManager:     stateManager,
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

	_ = teo.stateManager.StartToolExecution(toolCalls)

	logger.Info("Starting tool execution session",
		"session_id", sessionID,
		"request_id", requestID,
		"tool_count", len(toolCalls),
	)

	return sessionID, tea.Batch(
		func() tea.Msg {
			return domain.ToolExecutionStartedEvent{
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
			return domain.ShowErrorEvent{
				Error:  "Internal error: no active tool execution context",
				Sticky: false,
			}
		}

		if execution.CurrentIndex >= len(execution.ToolCalls) {
			return teo.completeExecution()()
		}

		currentTool := execution.ToolCalls[execution.CurrentIndex]

		approvalRequired := teo.isApprovalRequired(currentTool.Function.Name)

		teo.mutex.Lock()
		if approvalRequired {
			execution.Status = ToolExecutionStatusWaitingApproval
		} else {
			execution.Status = ToolExecutionStatusProcessing
		}
		teo.mutex.Unlock()

		progressMsg := domain.ToolExecutionProgressEvent{
			SessionID:        execution.SessionID,
			CurrentTool:      execution.CurrentIndex + 1,
			TotalTools:       len(execution.ToolCalls),
			ToolName:         currentTool.Function.Name,
			Status:           execution.Status.String(),
			RequiresApproval: approvalRequired,
		}

		if approvalRequired {
			return tea.Batch(
				func() tea.Msg { return progressMsg },
				func() tea.Msg {
					return domain.ToolApprovalRequestEvent{
						SessionID:  execution.SessionID,
						ToolCall:   currentTool,
						ToolIndex:  execution.CurrentIndex,
						TotalTools: len(execution.ToolCalls),
					}
				},
			)()
		} else {
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
			return domain.ShowErrorEvent{
				Error:  "Invalid approval response: no matching tool execution",
				Sticky: false,
			}
		}

		currentTool := execution.ToolCalls[execution.CurrentIndex]

		if approved {
			teo.mutex.Lock()
			execution.Status = ToolExecutionStatusExecuting
			teo.mutex.Unlock()

			return teo.executeTool(toolIndex)()
		} else {
			result := &domain.ToolExecutionResult{
				ToolName:  currentTool.Function.Name,
				Arguments: parseToolArguments(currentTool.Function.Arguments),
				Success:   false,
				Duration:  0,
				Error:     "Denied by user",
			}

			teo.mutex.Lock()
			execution.Results[execution.CurrentIndex] = result
			execution.Status = ToolExecutionStatusCancelled
			teo.mutex.Unlock()

			teo.addToolResultToConversation(currentTool, result)
			teo.stateManager.EndToolExecution()

			teo.currentExecution = nil

			return domain.SetStatusEvent{
				Message: "Tool execution cancelled due to user denial",
				Spinner: false,
			}
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
			return domain.ShowErrorEvent{
				Error:  "Invalid tool execution request",
				Sticky: false,
			}
		}

		currentTool := execution.ToolCalls[toolIndex]
		startTime := time.Now()

		args := parseToolArguments(currentTool.Function.Arguments)

		var executionResult *domain.ToolExecutionResult

		if teo.shouldSkipToolExecution(currentTool.Function.Name) {
			duration := time.Since(startTime)
			executionResult = teo.createSkippedToolResult(currentTool.Function.Name, args, duration)
		} else {
			ctx := context.Background()
			result, err := teo.toolService.ExecuteTool(ctx, currentTool.Function.Name, args)

			duration := time.Since(startTime)

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
		}

		teo.mutex.Lock()
		execution.Results[toolIndex] = executionResult
		execution.CurrentIndex++
		execution.Status = ToolExecutionStatusProcessing
		teo.mutex.Unlock()

		teo.addToolResultToConversation(currentTool, executionResult)

		return teo.processNextTool()()
	}
}

// completeExecution completes the tool execution session
func (teo *ToolExecutionOrchestrator) completeExecution() tea.Cmd {
	return func() tea.Msg {
		teo.mutex.Lock()
		defer teo.mutex.Unlock()

		if teo.currentExecution == nil {
			return domain.ShowErrorEvent{
				Error:  "No active execution to complete",
				Sticky: false,
			}
		}

		execution := teo.currentExecution
		execution.Status = ToolExecutionStatusCompleted

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

		logger.Info("Tool execution session completed",
			"session_id", execution.SessionID,
			"total_tools", len(execution.ToolCalls),
			"success_count", successCount,
			"failure_count", failureCount,
			"duration", time.Since(execution.StartTime).String(),
		)

		teo.stateManager.EndToolExecution()

		sessionID := execution.SessionID
		results := execution.Results
		teo.currentExecution = nil

		return domain.ToolExecutionCompletedEvent{
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
			return nil
		}

		execution := teo.currentExecution
		execution.Status = ToolExecutionStatusCancelled

		teo.stateManager.EndToolExecution()

		teo.currentExecution = nil

		return domain.SetStatusEvent{
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

	copy := *teo.currentExecution
	return true, &copy
}

// isApprovalRequired checks if a tool requires approval
func (teo *ToolExecutionOrchestrator) isApprovalRequired(toolName string) bool {
	if teo.configService == nil {
		return true
	}

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
func parseToolArguments(arguments string) map[string]any {
	var args map[string]any
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		logger.Error("Failed to parse tool arguments", "error", err, "arguments", arguments)
		return make(map[string]any)
	}
	return args
}

// generateSessionID generates a unique session ID
func generateSessionID() string {
	return fmt.Sprintf("session_%d", time.Now().UnixNano())
}

// RecoverFromStuckState attempts to recover from a stuck tool execution state
func (teo *ToolExecutionOrchestrator) RecoverFromStuckState() tea.Cmd {
	return func() tea.Msg {
		teo.mutex.Lock()
		defer teo.mutex.Unlock()

		if teo.currentExecution == nil {
			return nil
		}

		execution := teo.currentExecution

		now := time.Now()
		if now.Sub(execution.StartTime) > 5*time.Minute {
			logger.Warn("Detected stuck tool execution, attempting recovery",
				"session_id", execution.SessionID,
				"current_index", execution.CurrentIndex,
				"status", execution.Status.String(),
			)

			execution.Status = ToolExecutionStatusCancelled
			teo.stateManager.EndToolExecution()
			teo.currentExecution = nil

			return domain.SetStatusEvent{
				Message: "Recovered from stuck tool execution",
				Spinner: false,
			}
		}

		return nil
	}
}

// GetHealthStatus returns the health status of the tool execution orchestrator
func (teo *ToolExecutionOrchestrator) GetHealthStatus() map[string]any {
	teo.mutex.RLock()
	defer teo.mutex.RUnlock()

	status := map[string]any{
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

		if time.Since(teo.currentExecution.StartTime) > 5*time.Minute {
			status["healthy"] = false
			status["issue"] = "execution_stuck"
		}
	}

	return status
}

// shouldSkipToolExecution determines if a tool should be skipped for execution
// A2A and MCP tools are executed on the Gateway, not on clients when skip is enabled
func (teo *ToolExecutionOrchestrator) shouldSkipToolExecution(toolName string) bool {
	if teo.configService == nil {
		return strings.HasPrefix(toolName, "a2a_") || strings.HasPrefix(toolName, "mcp_")
	}

	if strings.HasPrefix(toolName, "a2a_") {
		return teo.configService.ShouldSkipA2AToolOnClient()
	}

	if strings.HasPrefix(toolName, "mcp_") {
		return teo.configService.ShouldSkipMCPToolOnClient()
	}

	return false
}

// createSkippedToolResult creates a result for tools that are skipped for visualization
func (teo *ToolExecutionOrchestrator) createSkippedToolResult(toolName string, args map[string]any, duration time.Duration) *domain.ToolExecutionResult {
	var toolType string
	if strings.HasPrefix(toolName, "a2a_") {
		toolType = "A2A"
	} else if strings.HasPrefix(toolName, "mcp_") {
		toolType = "MCP"
	}

	return &domain.ToolExecutionResult{
		ToolName:  toolName,
		Arguments: args,
		Success:   true,
		Duration:  duration,
		Data: map[string]any{
			"type":        toolType,
			"status":      "executed_on_gateway",
			"description": fmt.Sprintf("%s tool call was executed on the Gateway middleware", toolType),
		},
		Metadata: map[string]string{
			"execution_location": "gateway",
			"tool_type":          toolType,
			"skipped_reason":     "client_visualization_only",
		},
	}
}
