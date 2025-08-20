package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/logger"
)

// DebugService provides comprehensive debugging capabilities for TUI flow understanding
type DebugService struct {
	enabled bool
	mutex   sync.RWMutex

	// Event tracking
	events    []DebugEvent
	maxEvents int

	// State tracking
	stateManager *StateManager

	// Performance metrics
	metrics *PerformanceMetrics

	// File output
	outputDir string
}

// DebugEvent represents a debug event in the TUI flow
type DebugEvent struct {
	ID         string                `json:"id"`
	Timestamp  time.Time             `json:"timestamp"`
	Type       DebugEventType        `json:"type"`
	Source     string                `json:"source"`
	Message    string                `json:"message"`
	Data       map[string]any        `json:"data"`
	StateAfter *domain.StateSnapshot `json:"state_after,omitempty"`
	Duration   *time.Duration        `json:"duration,omitempty"`
}

// DebugEventType represents the type of debug event
type DebugEventType int

const (
	DebugEventTypeKeyPress DebugEventType = iota
	DebugEventTypeMessage
	DebugEventTypeStateChange
	DebugEventTypeUIUpdate
	DebugEventTypeCommand
	DebugEventTypeError
	DebugEventTypePerformance
	DebugEventTypeSDKEvent
	DebugEventTypeToolExecution
)

func (d DebugEventType) String() string {
	switch d {
	case DebugEventTypeKeyPress:
		return "KeyPress"
	case DebugEventTypeMessage:
		return "Message"
	case DebugEventTypeStateChange:
		return "StateChange"
	case DebugEventTypeUIUpdate:
		return "UIUpdate"
	case DebugEventTypeCommand:
		return "Command"
	case DebugEventTypeError:
		return "Error"
	case DebugEventTypePerformance:
		return "Performance"
	case DebugEventTypeSDKEvent:
		return "SDKEvent"
	case DebugEventTypeToolExecution:
		return "ToolExecution"
	default:
		return "Unknown"
	}
}

// PerformanceMetrics tracks performance metrics for debugging
type PerformanceMetrics struct {
	MessageProcessingTimes map[string][]time.Duration
	UIRenderTimes          []time.Duration
	StateTransitionTimes   []time.Duration
	SDKCallTimes           map[string][]time.Duration
	mutex                  sync.RWMutex
}

// NewDebugService creates a new debug service
func NewDebugService(enabled bool, stateManager *StateManager, outputDir string) *DebugService {
	ds := &DebugService{
		enabled:      enabled,
		events:       make([]DebugEvent, 0),
		maxEvents:    1000,
		stateManager: stateManager,
		metrics:      NewPerformanceMetrics(),
		outputDir:    outputDir,
	}

	if enabled {
		ds.setupLogging()
		ds.stateManager.AddListener(ds)
	}

	return ds
}

// NewPerformanceMetrics creates a new performance metrics tracker
func NewPerformanceMetrics() *PerformanceMetrics {
	return &PerformanceMetrics{
		MessageProcessingTimes: make(map[string][]time.Duration),
		UIRenderTimes:          make([]time.Duration, 0),
		StateTransitionTimes:   make([]time.Duration, 0),
		SDKCallTimes:           make(map[string][]time.Duration),
	}
}

// setupLogging sets up the output directory for exports
func (ds *DebugService) setupLogging() {
	if ds.outputDir == "" {
		ds.outputDir = ".infer/logs"
	} else {
		ds.outputDir = filepath.Join(ds.outputDir, "logs")
	}

	_ = os.MkdirAll(ds.outputDir, 0755)
	logger.Debug("Debug service initialized", "outputDir", ds.outputDir)
}

// Enable enables debug mode
func (ds *DebugService) Enable() {
	ds.mutex.Lock()
	defer ds.mutex.Unlock()

	if !ds.enabled {
		ds.enabled = true
		ds.setupLogging()
		ds.stateManager.AddListener(ds)
		ds.LogEvent(DebugEventTypeMessage, "DebugService", "Debug mode enabled", nil)
	}
}

// Disable disables debug mode
func (ds *DebugService) Disable() {
	ds.mutex.Lock()
	defer ds.mutex.Unlock()

	if ds.enabled {
		ds.enabled = false
		ds.stateManager.RemoveListener(ds)
		ds.LogEvent(DebugEventTypeMessage, "DebugService", "Debug mode disabled", nil)
	}
}

// IsEnabled returns whether debug mode is enabled
func (ds *DebugService) IsEnabled() bool {
	ds.mutex.RLock()
	defer ds.mutex.RUnlock()
	return ds.enabled
}

// LogEvent logs a debug event
func (ds *DebugService) LogEvent(eventType DebugEventType, source, message string, data map[string]any) {
	if !ds.enabled {
		return
	}

	ds.mutex.Lock()
	defer ds.mutex.Unlock()

	event := DebugEvent{
		ID:        generateEventID(),
		Timestamp: time.Now(),
		Type:      eventType,
		Source:    source,
		Message:   message,
		Data:      data,
	}

	if ds.stateManager != nil && ds.shouldIncludeState(eventType, source) {
		state := ds.stateManager.GetStateSnapshot()
		event.StateAfter = &state
	}

	ds.events = append(ds.events, event)
	if len(ds.events) > ds.maxEvents {
		ds.events = ds.events[1:]
	}

	ds.logEventToLogger(event)

	logger.Debug("Debug event",
		"id", event.ID,
		"type", event.Type.String(),
		"source", event.Source,
		"message", event.Message,
	)
}

// LogKeyPress logs a key press event
func (ds *DebugService) LogKeyPress(key string, handler string, view string) {
	data := map[string]any{
		"key":     key,
		"handler": handler,
		"view":    view,
	}

	ds.LogEvent(DebugEventTypeKeyPress, "Input",
		fmt.Sprintf("Key '%s' handled by '%s' in view '%s'", key, handler, view), data)
}

// LogMessage logs a Bubble Tea message
func (ds *DebugService) LogMessage(msg tea.Msg, source string) {
	msgType := fmt.Sprintf("%T", msg)

	if ds.isNoisyMessage(msgType) {
		return
	}

	msgStr := fmt.Sprintf("%+v", msg)

	if len(msgStr) > 200 {
		msgStr = msgStr[:197] + "..."
	}

	data := map[string]any{
		"message_type": msgType,
		"message_data": msgStr,
	}

	ds.LogEvent(DebugEventTypeMessage, source,
		fmt.Sprintf("Processing message: %s", msgType), data)
}

// isNoisyMessage determines if a message type should be completely filtered out
func (ds *DebugService) isNoisyMessage(msgType string) bool {
	noisyTypes := []string{
		"spinner.TickMsg",          // Spinner animation frames - too frequent
		"tea.WindowSizeMsg",        // Window resize events (when not changing)
		"viewport.SyncMsg",         // Viewport sync messages
		"tea.MouseMsg",             // Mouse movement events
		"textinput.cursorBlinkMsg", // Text input cursor blink
	}

	for _, noisyType := range noisyTypes {
		if msgType == noisyType {
			return true
		}
	}

	return false
}

// shouldIncludeState determines if state snapshots should be included for this event type
// to reduce log verbosity
func (ds *DebugService) shouldIncludeState(eventType DebugEventType, source string) bool {
	switch eventType {
	case DebugEventTypeStateChange:
		return true // Always include state for state changes
	case DebugEventTypeError:
		return true // Always include state for errors
	case DebugEventTypeCommand:
		return true // Include state for commands
	case DebugEventTypeSDKEvent:
		return true // Include state for SDK events
	case DebugEventTypeToolExecution:
		return true // Include state for tool execution
	case DebugEventTypeMessage:
		// Only include state for non-router messages to reduce noise
		return source != "MessageRouter"
	default:
		return false // Don't include state for other event types
	}
}

// LogCommand logs a command execution
func (ds *DebugService) LogCommand(command string, args []string, result any, duration time.Duration) {
	data := map[string]any{
		"command":  command,
		"args":     args,
		"result":   result,
		"duration": duration.String(),
	}

	event := DebugEvent{
		ID:        generateEventID(),
		Timestamp: time.Now(),
		Type:      DebugEventTypeCommand,
		Source:    "CommandExecution",
		Message:   fmt.Sprintf("Executed command: %s", command),
		Data:      data,
		Duration:  &duration,
	}

	ds.addEvent(event)
}

// LogError logs an error event
func (ds *DebugService) LogError(err error, source string, context map[string]any) {
	data := map[string]any{
		"error":   err.Error(),
		"context": context,
	}

	ds.LogEvent(DebugEventTypeError, source,
		fmt.Sprintf("Error occurred: %s", err.Error()), data)
}

// LogSDKEvent logs an SDK event
func (ds *DebugService) LogSDKEvent(eventType string, requestID string, data any) {
	eventData := map[string]any{
		"event_type": eventType,
		"request_id": requestID,
		"event_data": data,
	}

	ds.LogEvent(DebugEventTypeSDKEvent, "SDK",
		fmt.Sprintf("SDK event: %s (req: %s)", eventType, requestID), eventData)
}

// LogToolExecution logs tool execution events
func (ds *DebugService) LogToolExecution(toolName string, phase string, data map[string]any) {
	eventData := map[string]any{
		"tool_name": toolName,
		"phase":     phase,
	}

	// Merge additional data
	for k, v := range data {
		eventData[k] = v
	}

	ds.LogEvent(DebugEventTypeToolExecution, "ToolExecution",
		fmt.Sprintf("Tool %s: %s", toolName, phase), eventData)
}

// TrackMessageProcessing tracks message processing time
func (ds *DebugService) TrackMessageProcessing(msgType string, duration time.Duration) {
	if !ds.enabled {
		return
	}

	ds.metrics.mutex.Lock()
	defer ds.metrics.mutex.Unlock()

	if ds.metrics.MessageProcessingTimes[msgType] == nil {
		ds.metrics.MessageProcessingTimes[msgType] = make([]time.Duration, 0)
	}

	ds.metrics.MessageProcessingTimes[msgType] = append(
		ds.metrics.MessageProcessingTimes[msgType], duration)

	if len(ds.metrics.MessageProcessingTimes[msgType]) > 100 {
		ds.metrics.MessageProcessingTimes[msgType] =
			ds.metrics.MessageProcessingTimes[msgType][1:]
	}
}

// TrackUIRender tracks UI render time
func (ds *DebugService) TrackUIRender(duration time.Duration) {
	if !ds.enabled {
		return
	}

	ds.metrics.mutex.Lock()
	defer ds.metrics.mutex.Unlock()

	ds.metrics.UIRenderTimes = append(ds.metrics.UIRenderTimes, duration)

	if len(ds.metrics.UIRenderTimes) > 100 {
		ds.metrics.UIRenderTimes = ds.metrics.UIRenderTimes[1:]
	}
}

// TrackStateTransition tracks state transition time
func (ds *DebugService) TrackStateTransition(duration time.Duration) {
	if !ds.enabled {
		return
	}

	ds.metrics.mutex.Lock()
	defer ds.metrics.mutex.Unlock()

	ds.metrics.StateTransitionTimes = append(ds.metrics.StateTransitionTimes, duration)

	if len(ds.metrics.StateTransitionTimes) > 100 {
		ds.metrics.StateTransitionTimes = ds.metrics.StateTransitionTimes[1:]
	}
}

// TrackSDKCall tracks SDK call time
func (ds *DebugService) TrackSDKCall(operation string, duration time.Duration) {
	if !ds.enabled {
		return
	}

	ds.metrics.mutex.Lock()
	defer ds.metrics.mutex.Unlock()

	if ds.metrics.SDKCallTimes[operation] == nil {
		ds.metrics.SDKCallTimes[operation] = make([]time.Duration, 0)
	}

	ds.metrics.SDKCallTimes[operation] = append(
		ds.metrics.SDKCallTimes[operation], duration)

	// Keep only last 100 measurements per operation
	if len(ds.metrics.SDKCallTimes[operation]) > 100 {
		ds.metrics.SDKCallTimes[operation] =
			ds.metrics.SDKCallTimes[operation][1:]
	}
}

// OnStateChanged implements StateChangeListener
func (ds *DebugService) OnStateChanged(oldState, newState domain.StateSnapshot) {
	if !ds.enabled {
		return
	}

	changes := ds.detectStateChanges(oldState, newState)
	if len(changes) > 0 {
		data := map[string]any{
			"old_state": oldState,
			"new_state": newState,
			"changes":   changes,
		}

		ds.LogEvent(DebugEventTypeStateChange, "StateManager",
			fmt.Sprintf("State changed: %s", strings.Join(changes, ", ")), data)
	}
}

// detectStateChanges detects what changed between two state snapshots
func (ds *DebugService) detectStateChanges(oldState, newState domain.StateSnapshot) []string {
	var changes []string

	if oldState.CurrentView != newState.CurrentView {
		changes = append(changes, fmt.Sprintf("view: %s -> %s",
			oldState.CurrentView, newState.CurrentView))
	}

	if oldState.ChatSession == nil && newState.ChatSession != nil {
		changes = append(changes, "chat session started")
	} else if oldState.ChatSession != nil && newState.ChatSession == nil {
		changes = append(changes, "chat session ended")
	} else if oldState.ChatSession != nil && newState.ChatSession != nil {
		if oldState.ChatSession.Status != newState.ChatSession.Status {
			changes = append(changes, fmt.Sprintf("chat status: %s -> %s",
				oldState.ChatSession.Status, newState.ChatSession.Status))
		}
	}

	if oldState.ToolExecution == nil && newState.ToolExecution != nil {
		changes = append(changes, "tool execution started")
	} else if oldState.ToolExecution != nil && newState.ToolExecution == nil {
		changes = append(changes, "tool execution ended")
	} else if oldState.ToolExecution != nil && newState.ToolExecution != nil {
		if oldState.ToolExecution.Status != newState.ToolExecution.Status {
			changes = append(changes, fmt.Sprintf("tool status: %s -> %s",
				oldState.ToolExecution.Status, newState.ToolExecution.Status))
		}
	}

	return changes
}

// ExportEvents exports debug events to a file
func (ds *DebugService) ExportEvents(filename string) error {
	ds.mutex.RLock()
	defer ds.mutex.RUnlock()

	data, err := json.MarshalIndent(ds.events, "", "  ")
	if err != nil {
		return err
	}

	filepath := filepath.Join(ds.outputDir, filename)
	return os.WriteFile(filepath, data, 0644)
}

// ExportMetrics exports performance metrics to a file
func (ds *DebugService) ExportMetrics(filename string) error {
	ds.metrics.mutex.RLock()
	defer ds.metrics.mutex.RUnlock()

	summary := ds.generateMetricsSummary()
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}

	filepath := filepath.Join(ds.outputDir, filename)
	return os.WriteFile(filepath, data, 0644)
}

// generateMetricsSummary generates a summary of performance metrics
func (ds *DebugService) generateMetricsSummary() map[string]any {
	summary := make(map[string]any)

	msgSummary := make(map[string]any)
	for msgType, times := range ds.metrics.MessageProcessingTimes {
		if len(times) > 0 {
			avg := calculateAverage(times)
			max := calculateMax(times)
			min := calculateMin(times)

			msgSummary[msgType] = map[string]any{
				"count":  len(times),
				"avg_ms": avg.Milliseconds(),
				"max_ms": max.Milliseconds(),
				"min_ms": min.Milliseconds(),
			}
		}
	}
	summary["message_processing"] = msgSummary

	if len(ds.metrics.UIRenderTimes) > 0 {
		avg := calculateAverage(ds.metrics.UIRenderTimes)
		max := calculateMax(ds.metrics.UIRenderTimes)
		min := calculateMin(ds.metrics.UIRenderTimes)

		summary["ui_rendering"] = map[string]any{
			"count":  len(ds.metrics.UIRenderTimes),
			"avg_ms": avg.Milliseconds(),
			"max_ms": max.Milliseconds(),
			"min_ms": min.Milliseconds(),
		}
	}

	if len(ds.metrics.StateTransitionTimes) > 0 {
		avg := calculateAverage(ds.metrics.StateTransitionTimes)
		max := calculateMax(ds.metrics.StateTransitionTimes)
		min := calculateMin(ds.metrics.StateTransitionTimes)

		summary["state_transitions"] = map[string]any{
			"count":  len(ds.metrics.StateTransitionTimes),
			"avg_ms": avg.Milliseconds(),
			"max_ms": max.Milliseconds(),
			"min_ms": min.Milliseconds(),
		}
	}

	sdkSummary := make(map[string]any)
	for operation, times := range ds.metrics.SDKCallTimes {
		if len(times) > 0 {
			avg := calculateAverage(times)
			max := calculateMax(times)
			min := calculateMin(times)

			sdkSummary[operation] = map[string]any{
				"count":  len(times),
				"avg_ms": avg.Milliseconds(),
				"max_ms": max.Milliseconds(),
				"min_ms": min.Milliseconds(),
			}
		}
	}
	summary["sdk_calls"] = sdkSummary

	return summary
}

// GetEventsSince returns events since a specific timestamp
func (ds *DebugService) GetEventsSince(since time.Time) []DebugEvent {
	ds.mutex.RLock()
	defer ds.mutex.RUnlock()

	var events []DebugEvent
	for _, event := range ds.events {
		if event.Timestamp.After(since) {
			events = append(events, event)
		}
	}
	return events
}

// GetEventsByType returns events of a specific type
func (ds *DebugService) GetEventsByType(eventType DebugEventType) []DebugEvent {
	ds.mutex.RLock()
	defer ds.mutex.RUnlock()

	var events []DebugEvent
	for _, event := range ds.events {
		if event.Type == eventType {
			events = append(events, event)
		}
	}
	return events
}

// GetPerformanceSummary returns a performance summary
func (ds *DebugService) GetPerformanceSummary() map[string]any {
	ds.metrics.mutex.RLock()
	defer ds.metrics.mutex.RUnlock()

	return ds.generateMetricsSummary()
}

// Cleanup closes resources and saves final debug data
func (ds *DebugService) Cleanup() {
	if !ds.enabled {
		return
	}

	ds.mutex.Lock()
	defer ds.mutex.Unlock()

	timestamp := time.Now().Format("20060102_150405")
	_ = ds.ExportEvents(fmt.Sprintf("events_final_%s.json", timestamp))
	_ = ds.ExportMetrics(fmt.Sprintf("metrics_final_%s.json", timestamp))

	logger.Debug("Debug service cleanup completed")
}

func (ds *DebugService) addEvent(event DebugEvent) {
	ds.mutex.Lock()
	defer ds.mutex.Unlock()

	if ds.stateManager != nil {
		state := ds.stateManager.GetStateSnapshot()
		event.StateAfter = &state
	}

	ds.events = append(ds.events, event)
	if len(ds.events) > ds.maxEvents {
		ds.events = ds.events[1:]
	}

	ds.logEventToLogger(event)
}

func (ds *DebugService) logEventToLogger(event DebugEvent) {
	fields := []any{
		"event_id", event.ID,
		"event_type", event.Type.String(),
		"source", event.Source,
		"timestamp", event.Timestamp,
	}

	if event.Duration != nil {
		fields = append(fields, "duration_ms", event.Duration.Milliseconds())
	}

	if event.Data != nil {
		for k, v := range event.Data {
			fields = append(fields, k, v)
		}
	}

	switch event.Type {
	case DebugEventTypeError:
		logger.Error(event.Message, fields...)
	case DebugEventTypePerformance:
		logger.Info(event.Message, fields...)
	default:
		logger.Debug(event.Message, fields...)
	}
}

func generateEventID() string {
	return fmt.Sprintf("evt_%d", time.Now().UnixNano())
}

func calculateAverage(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}

	var total time.Duration
	for _, d := range durations {
		total += d
	}
	return total / time.Duration(len(durations))
}

func calculateMax(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}

	max := durations[0]
	for _, d := range durations[1:] {
		if d > max {
			max = d
		}
	}
	return max
}

func calculateMin(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}

	min := durations[0]
	for _, d := range durations[1:] {
		if d < min {
			min = d
		}
	}
	return min
}
