package handlers

import (
	"fmt"
	"reflect"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/services"
	"github.com/inference-gateway/cli/internal/ui/shared"
)

// MessageRouter routes messages to handlers using the new state management system
type MessageRouter struct {
	handlers []MessageHandler
}

// NewMessageRouter creates a new message router
func NewMessageRouter() *MessageRouter {
	return &MessageRouter{
		handlers: make([]MessageHandler, 0),
	}
}

// AddHandler adds a message handler to the router
func (r *MessageRouter) AddHandler(handler MessageHandler) {
	r.handlers = append(r.handlers, handler)

	// Sort handlers by priority (highest priority first)
	sort.Slice(r.handlers, func(i, j int) bool {
		return r.handlers[i].GetPriority() > r.handlers[j].GetPriority()
	})
}

// RemoveHandler removes a message handler from the router
func (r *MessageRouter) RemoveHandler(handlerName string) {
	for i, handler := range r.handlers {
		if handler.GetName() == handlerName {
			r.handlers = append(r.handlers[:i], r.handlers[i+1:]...)
			break
		}
	}
}

// Route routes a message to the appropriate handler
func (r *MessageRouter) Route(
	msg tea.Msg,
	stateManager *services.StateManager,
	debugService *services.DebugService,
) (tea.Model, tea.Cmd) {
	start := time.Now()
	msgType := getMessageType(msg)

	// Find the first handler that can handle this message
	for _, handler := range r.handlers {
		if handler.CanHandle(msg) {
			// Only log significant message routing (not every single message)
			if debugService != nil && debugService.IsEnabled() && isSignificantMessage(msgType) {
				debugService.LogEvent(
					services.DebugEventTypeMessage,
					"MessageRouter",
					"Message routed to handler: "+handler.GetName(),
					map[string]any{
						"message_type": msgType,
						"handler":      handler.GetName(),
						"priority":     handler.GetPriority(),
					},
				)
			}

			// Handle the message
			model, cmd := handler.Handle(msg, stateManager, debugService)

			// Track performance for significant messages only
			duration := time.Since(start)
			if debugService != nil && isSignificantMessage(msgType) {
				debugService.TrackMessageProcessing(msgType, duration)
			}

			return model, cmd
		}
	}

	// Only log when no handler found for significant messages that aren't handled by view-specific handlers
	if debugService != nil && debugService.IsEnabled() && isSignificantMessage(msgType) && !isViewHandledMessage(msgType) {
		debugService.LogEvent(
			services.DebugEventTypeMessage,
			"MessageRouter",
			"No handler found for message",
			map[string]any{
				"message_type": msgType,
			},
		)
	}

	return nil, nil
}

// GetHandlers returns all registered handlers
func (r *MessageRouter) GetHandlers() []MessageHandler {
	// Return a copy to prevent external modification
	handlers := make([]MessageHandler, len(r.handlers))
	copy(handlers, r.handlers)
	return handlers
}

// GetHandlerByName returns a handler by name
func (r *MessageRouter) GetHandlerByName(name string) MessageHandler {
	for _, handler := range r.handlers {
		if handler.GetName() == name {
			return handler
		}
	}
	return nil
}

// GetHandlerCount returns the number of registered handlers
func (r *MessageRouter) GetHandlerCount() int {
	return len(r.handlers)
}

// Helper functions

// messageTypeRegistry maps message types to their string representations
var messageTypeRegistry = map[reflect.Type]string{
	reflect.TypeOf(tea.KeyMsg{}):                     "tea.KeyMsg",
	reflect.TypeOf(tea.WindowSizeMsg{}):              "tea.WindowSizeMsg",
	reflect.TypeOf(tea.MouseMsg{}):                   "tea.MouseMsg",
	reflect.TypeOf(tea.QuitMsg{}):                    "tea.QuitMsg",
	reflect.TypeOf(domain.ChatStartEvent{}):          "domain.ChatStartEvent",
	reflect.TypeOf(domain.ChatChunkEvent{}):          "domain.ChatChunkEvent",
	reflect.TypeOf(domain.ToolCallStartEvent{}):      "domain.ToolCallStartEvent",
	reflect.TypeOf(domain.ChatCompleteEvent{}):       "domain.ChatCompleteEvent",
	reflect.TypeOf(domain.ChatErrorEvent{}):          "domain.ChatErrorEvent",
	reflect.TypeOf(domain.ToolCallEvent{}):           "domain.ToolCallEvent",
	reflect.TypeOf(domain.CancelledEvent{}):          "domain.CancelledEvent",
	reflect.TypeOf(shared.UpdateHistoryMsg{}):        "shared.UpdateHistoryMsg",
	reflect.TypeOf(shared.SetStatusMsg{}):            "shared.SetStatusMsg",
	reflect.TypeOf(shared.UpdateStatusMsg{}):         "shared.UpdateStatusMsg",
	reflect.TypeOf(shared.ShowErrorMsg{}):            "shared.ShowErrorMsg",
	reflect.TypeOf(shared.ClearErrorMsg{}):           "shared.ClearErrorMsg",
	reflect.TypeOf(shared.ClearInputMsg{}):           "shared.ClearInputMsg",
	reflect.TypeOf(shared.SetInputMsg{}):             "shared.SetInputMsg",
	reflect.TypeOf(shared.UserInputMsg{}):            "shared.UserInputMsg",
	reflect.TypeOf(shared.ModelSelectedMsg{}):        "shared.ModelSelectedMsg",
	reflect.TypeOf(shared.FileSelectedMsg{}):         "shared.FileSelectedMsg",
	reflect.TypeOf(shared.FileSelectionRequestMsg{}): "shared.FileSelectionRequestMsg",
	reflect.TypeOf(shared.SetupFileSelectionMsg{}):   "shared.SetupFileSelectionMsg",
	reflect.TypeOf(shared.ApprovalRequestMsg{}):      "shared.ApprovalRequestMsg",
	reflect.TypeOf(shared.ApprovalResponseMsg{}):     "shared.ApprovalResponseMsg",
	reflect.TypeOf(shared.ScrollRequestMsg{}):        "shared.ScrollRequestMsg",
	reflect.TypeOf(shared.FocusRequestMsg{}):         "shared.FocusRequestMsg",
	reflect.TypeOf(shared.ResizeMsg{}):               "shared.ResizeMsg",
	reflect.TypeOf(shared.DebugKeyMsg{}):             "shared.DebugKeyMsg",
	reflect.TypeOf(shared.ToggleHelpBarMsg{}):        "shared.ToggleHelpBarMsg",
	reflect.TypeOf(shared.HideHelpBarMsg{}):          "shared.HideHelpBarMsg",
}

func getMessageType(msg tea.Msg) string {
	msgType := reflect.TypeOf(msg)
	if typeName, exists := messageTypeRegistry[msgType]; exists {
		return typeName
	}
	return fmt.Sprintf("%T", msg)
}

// isViewHandledMessage determines if a message is typically handled by view-specific handlers
// These messages don't need to be logged as "unhandled" by the message router
func isViewHandledMessage(msgType string) bool {
	switch msgType {
	case "tea.KeyMsg":
		return true // Key messages are handled by view-specific handlers (model selector, input view, etc.)
	case "tea.MouseMsg":
		return true // Mouse messages are handled by view-specific handlers
	case "shared.ModelSelectedMsg":
		return true // Model selection messages are handled in view-specific logic
	case "shared.ScrollRequestMsg":
		return true // Scroll requests are handled by UI components
	case "shared.FocusRequestMsg":
		return true // Focus requests are handled by UI components
	default:
		return false
	}
}

// significantMessageMap maps message types to their significance for logging
var significantMessageMap = map[string]bool{
	"spinner.TickMsg":                false, // Spinner updates are too frequent
	"*spinner.TickMsg":               false, // Spinner updates are too frequent
	"tea.KeyMsg":                     true,  // User input is significant
	"tea.MouseMsg":                   true,  // User interaction is significant
	"tea.WindowSizeMsg":              false, // Window resize events are not significant
	"tea.PasteMsg":                   true,  // User paste is significant
	"shared.UpdateHistoryMsg":        false, // History updates are frequent and not significant for debugging
	"shared.SetStatusMsg":            false, // Status updates are usually frequent
	"shared.UpdateStatusMsg":         false, // Status updates are usually frequent
	"shared.ShowErrorMsg":            true,  // Errors are significant
	"shared.ClearErrorMsg":           false, // Clear operations are not significant
	"shared.ClearInputMsg":           false, // Clear operations are not significant
	"shared.SetInputMsg":             false, // Input updates are frequent
	"shared.UserInputMsg":            true,  // User input is significant
	"shared.ModelSelectedMsg":        true,  // Model selection is significant
	"shared.FileSelectedMsg":         true,  // File selection is significant
	"shared.FileSelectionRequestMsg": true,  // File selection requests are significant
	"shared.SetupFileSelectionMsg":   true,  // File selection requests are significant
	"shared.ApprovalRequestMsg":      true,  // Approval requests are significant
	"shared.ApprovalResponseMsg":     true,  // Approval responses are significant
	"shared.ScrollRequestMsg":        false, // Scroll requests are frequent and not significant
	"shared.FocusRequestMsg":         false, // Focus changes are not significant for debugging
	"shared.ResizeMsg":               false, // Resize events are not significant
	"shared.DebugKeyMsg":             false, // Debug key messages are meta and not significant for business logic
	"shared.ToggleHelpBarMsg":        false, // Help bar toggles are not significant
	"shared.HideHelpBarMsg":          false, // Help bar visibility changes are not significant
	"domain.ChatStartEvent":          true,  // Chat events are significant
	"domain.ChatCompleteEvent":       true,  // Chat events are significant
	"domain.ChatErrorEvent":          true,  // Errors are significant
	"domain.ToolCallStartEvent":      true,  // Tool calls are significant
	"domain.ToolCallEvent":           true,  // Tool calls are significant
}

// isSignificantMessage determines if a message is worth logging
// We only log meaningful user interactions and system events, not UI updates
func isSignificantMessage(msgType string) bool {
	if significance, exists := significantMessageMap[msgType]; exists {
		return significance
	}
	return true // Default to logging unknown message types
}
