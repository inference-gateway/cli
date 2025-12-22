package handlers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// TestMapStatusToAGUI tests the status mapping function
func TestMapStatusToAGUI(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"queued maps to queued", "queued", "queued"},
		{"ready maps to queued", "ready", "queued"},
		{"running maps to running", "running", "running"},
		{"starting maps to running", "starting", "running"},
		{"saving maps to running", "saving", "running"},
		{"executing maps to running", "executing", "running"},
		{"streaming maps to running", "streaming", "running"},
		{"complete maps to complete", "complete", "complete"},
		{"completed maps to complete", "completed", "complete"},
		{"executed maps to complete", "executed", "complete"},
		{"error maps to failed", "error", "failed"},
		{"failed maps to failed", "failed", "failed"},
		{"unknown passes through", "unknown", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapStatusToAGUI(tt.input)
			if result != tt.expected {
				t.Errorf("mapStatusToAGUI(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestTrackToolExecution tests tool execution state tracking
func TestTrackToolExecution(t *testing.T) {
	h := &HeadlessHandler{}

	callID := "call_123"
	toolName := "Bash"

	h.trackToolExecution(callID, toolName)

	state := h.getToolState(callID)
	if state == nil {
		t.Fatal("Expected state to be created, got nil")
	}

	if state.CallID != callID {
		t.Errorf("Expected CallID %q, got %q", callID, state.CallID)
	}

	if state.ToolName != toolName {
		t.Errorf("Expected ToolName %q, got %q", toolName, state.ToolName)
	}

	if state.Status != "queued" {
		t.Errorf("Expected initial status 'queued', got %q", state.Status)
	}

	if state.OutputBuffer == nil {
		t.Error("Expected OutputBuffer to be initialized, got nil")
	}

	if state.StartTime.IsZero() {
		t.Error("Expected StartTime to be set, got zero time")
	}
}

// TestUpdateToolStatus tests updating tool status
func TestUpdateToolStatus(t *testing.T) {
	h := &HeadlessHandler{}

	callID := "call_123"
	toolName := "Bash"

	h.trackToolExecution(callID, toolName)

	state := h.updateToolStatus(callID, "running")
	if state == nil {
		t.Fatal("Expected state to be returned, got nil")
	}

	if state.Status != "running" {
		t.Errorf("Expected status 'running', got %q", state.Status)
	}

	nilState := h.updateToolStatus("non_existent", "complete")
	if nilState != nil {
		t.Error("Expected nil for non-existent tool, got state")
	}
}

// TestRemoveToolState tests removing tool state
func TestRemoveToolState(t *testing.T) {
	h := &HeadlessHandler{}

	callID := "call_123"
	toolName := "Bash"

	h.trackToolExecution(callID, toolName)

	if h.getToolState(callID) == nil {
		t.Fatal("Expected state to exist before removal")
	}

	h.removeToolState(callID)

	if h.getToolState(callID) != nil {
		t.Error("Expected state to be removed, but it still exists")
	}
}

// TestConcurrentStateAccess tests thread-safe state access
func TestConcurrentStateAccess(t *testing.T) {
	h := &HeadlessHandler{}

	done := make(chan bool)
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			callID := "call_" + string(rune('0'+id))
			toolName := "Tool" + string(rune('0'+id))

			h.trackToolExecution(callID, toolName)
			h.updateToolStatus(callID, "running")
			_ = h.getToolState(callID)
			h.updateToolStatus(callID, "complete")
			h.removeToolState(callID)

			done <- true
		}(i)
	}

	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	h.toolStatesMux.RLock()
	remaining := len(h.toolStates)
	h.toolStatesMux.RUnlock()

	if remaining != 0 {
		t.Errorf("Expected 0 remaining states, got %d", remaining)
	}
}

// TestToolCallProgressEventEmission tests ToolCallProgress event emission
func TestToolCallProgressEventEmission(t *testing.T) {
	var stdout bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := &HeadlessHandler{
		stdout: &stdout,
		ctx:    ctx,
	}

	callID := "call_123"
	h.trackToolExecution(callID, "Bash")

	progressEvent := domain.ToolExecutionProgressEvent{
		ToolCallID: callID,
		ToolName:   "Bash",
		Status:     "running",
		Message:    "Executing command...",
	}

	events := make(chan domain.ChatEvent, 1)
	events <- progressEvent
	close(events)

	err := h.processStreamingEvents(events)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	scanner := bufio.NewScanner(&stdout)
	scanner.Split(bufio.ScanLines)

	var emittedProgress bool
	for scanner.Scan() {
		line := scanner.Bytes()
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}

		if event["type"] == "ToolCallProgress" {
			emittedProgress = true
			if event["toolCallId"] != callID {
				t.Errorf("Expected toolCallId %q, got %v", callID, event["toolCallId"])
			}
			if event["status"] != "running" {
				t.Errorf("Expected status 'running', got %v", event["status"])
			}
			if event["message"] != "Executing command..." {
				t.Errorf("Expected message 'Executing command...', got %v", event["message"])
			}
		}
	}

	if !emittedProgress {
		t.Error("Expected ToolCallProgress event to be emitted")
	}
}

// TestBashOutputChunkEventEmission tests BashOutputChunkEvent emission
func TestBashOutputChunkEventEmission(t *testing.T) {
	var stdout bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := &HeadlessHandler{
		stdout: &stdout,
		ctx:    ctx,
	}

	callID := "call_123"
	h.trackToolExecution(callID, "Bash")

	bashEvent := domain.BashOutputChunkEvent{
		ToolCallID: callID,
		Output:     "total 48\n",
		IsComplete: false,
	}

	events := make(chan domain.ChatEvent, 1)
	events <- bashEvent
	close(events)

	err := h.processStreamingEvents(events)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	scanner := bufio.NewScanner(&stdout)
	scanner.Split(bufio.ScanLines)

	var foundOutput bool
	for scanner.Scan() {
		line := scanner.Bytes()
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}

		if event["type"] != "ToolCallProgress" {
			continue
		}

		foundOutput = true
		if event["output"] != "total 48\n" {
			t.Errorf("Expected output 'total 48\\n', got %v", event["output"])
		}
		if metadata, ok := event["metadata"].(map[string]any); ok {
			if metadata["isComplete"] != false {
				t.Errorf("Expected isComplete false, got %v", metadata["isComplete"])
			}
		}
	}

	if !foundOutput {
		t.Error("Expected ToolCallProgress event with output to be emitted")
	}
}

// TestParallelToolsMetadataEmission tests ParallelToolsMetadata event emission
func TestParallelToolsMetadataEmission(t *testing.T) {
	var stdout bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := &HeadlessHandler{
		stdout: &stdout,
		ctx:    ctx,
	}

	completeEvent := domain.ParallelToolsCompleteEvent{
		TotalExecuted: 3,
		SuccessCount:  2,
		FailureCount:  1,
		Duration:      5 * time.Second,
	}

	events := make(chan domain.ChatEvent, 1)
	events <- completeEvent
	close(events)

	err := h.processStreamingEvents(events)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	scanner := bufio.NewScanner(&stdout)
	scanner.Split(bufio.ScanLines)

	var foundMetadata bool
	for scanner.Scan() {
		line := scanner.Bytes()
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}

		if event["type"] != "ParallelToolsMetadata" {
			continue
		}

		foundMetadata = true
		if int(event["totalCount"].(float64)) != 3 {
			t.Errorf("Expected totalCount 3, got %v", event["totalCount"])
		}
		if int(event["successCount"].(float64)) != 2 {
			t.Errorf("Expected successCount 2, got %v", event["successCount"])
		}
		if int(event["failureCount"].(float64)) != 1 {
			t.Errorf("Expected failureCount 1, got %v", event["failureCount"])
		}
		if event["totalDuration"].(float64) != 5.0 {
			t.Errorf("Expected totalDuration 5.0, got %v", event["totalDuration"])
		}
	}

	if !foundMetadata {
		t.Error("Expected ParallelToolsMetadata event to be emitted")
	}
}

// TestToolCallResultWithDuration tests ToolCallResult emission with duration
func TestToolCallResultWithDuration(t *testing.T) {
	var stdout bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := &HeadlessHandler{
		stdout: &stdout,
		ctx:    ctx,
	}

	callID := "call_123"
	h.trackToolExecution(callID, "Bash")

	completeEvent := domain.ToolExecutionProgressEvent{
		ToolCallID: callID,
		ToolName:   "Bash",
		Status:     "complete",
		Message:    "Command executed successfully",
	}

	events := make(chan domain.ChatEvent, 1)
	events <- completeEvent
	close(events)

	err := h.processStreamingEvents(events)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	scanner := bufio.NewScanner(&stdout)
	scanner.Split(bufio.ScanLines)

	var foundResult bool
	for scanner.Scan() {
		line := scanner.Bytes()
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}

		if event["type"] == "ToolCallResult" {
			foundResult = true
			if event["status"] != "complete" {
				t.Errorf("Expected status 'complete', got %v", event["status"])
			}
			if _, ok := event["duration"]; !ok {
				t.Error("Expected duration field to be present")
			}
			if event["duration"].(float64) < 0 {
				t.Errorf("Expected non-negative duration, got %v", event["duration"])
			}
		}
	}

	if !foundResult {
		t.Error("Expected ToolCallResult event to be emitted")
	}

	if h.getToolState(callID) != nil {
		t.Error("Expected tool state to be removed after completion")
	}
}
