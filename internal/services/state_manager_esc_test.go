package services

import (
	"testing"
	"time"
)

func TestStateManager_RecordEscPress_SinglePress(t *testing.T) {
	sm := NewStateManager(false)

	if result := sm.RecordEscPress(); result {
		t.Error("Single ESC press should not trigger double ESC")
	}

	if sm.escPressCount != 1 {
		t.Errorf("Expected escPressCount=1, got %d", sm.escPressCount)
	}
	if sm.lastEscPressTime.IsZero() {
		t.Error("lastEscPressTime should be set after first press")
	}
}

func TestStateManager_RecordEscPress_DoublePress(t *testing.T) {
	sm := NewStateManager(false)

	if result := sm.RecordEscPress(); result {
		t.Error("First ESC press should not trigger double ESC")
	}

	if result := sm.RecordEscPress(); !result {
		t.Error("Second ESC press within 300ms should trigger double ESC")
	}

	if sm.escPressCount != 0 {
		t.Errorf("escPressCount should be reset to 0, got %d", sm.escPressCount)
	}
	if !sm.lastEscPressTime.IsZero() {
		t.Error("lastEscPressTime should be reset to zero after double ESC")
	}
}

func TestStateManager_RecordEscPress_DoublePress_AfterTimeout(t *testing.T) {
	sm := NewStateManager(false)

	sm.RecordEscPress()

	time.Sleep(350 * time.Millisecond)

	if result := sm.RecordEscPress(); result {
		t.Error("Second ESC press after 300ms should not trigger double ESC")
	}

	if sm.escPressCount != 1 {
		t.Errorf("Expected escPressCount=1 after timeout, got %d", sm.escPressCount)
	}
}

func TestStateManager_RecordEscPress_TriplePress(t *testing.T) {
	sm := NewStateManager(false)

	if result := sm.RecordEscPress(); result {
		t.Error("First press should not trigger")
	}

	if result := sm.RecordEscPress(); !result {
		t.Error("Second press should trigger double ESC")
	}

	if result := sm.RecordEscPress(); result {
		t.Error("Third press should not trigger (state was reset)")
	}

	if sm.escPressCount != 1 {
		t.Errorf("Expected escPressCount=1 after triple press, got %d", sm.escPressCount)
	}
}

func TestStateManager_RecordEscPress_MultipleSessions(t *testing.T) {
	sm := NewStateManager(false)

	sm.RecordEscPress()
	if result := sm.RecordEscPress(); !result {
		t.Error("First double ESC should trigger")
	}

	time.Sleep(50 * time.Millisecond)

	sm.RecordEscPress()
	if result := sm.RecordEscPress(); !result {
		t.Error("Second double ESC should also trigger")
	}
}

func TestStateManager_RecordEscPress_ConcurrentAccess(t *testing.T) {
	sm := NewStateManager(false)

	done := make(chan bool)
	triggerCount := 0

	for i := 0; i < 10; i++ {
		go func() {
			if sm.RecordEscPress() {
				triggerCount++
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestStateManager_ResetEscTracking(t *testing.T) {
	sm := NewStateManager(false)

	sm.RecordEscPress()

	if sm.escPressCount == 0 {
		t.Error("escPressCount should be non-zero after press")
	}

	sm.ResetEscTracking()

	if sm.escPressCount != 0 {
		t.Errorf("escPressCount should be 0 after reset, got %d", sm.escPressCount)
	}
	if !sm.lastEscPressTime.IsZero() {
		t.Error("lastEscPressTime should be zero after reset")
	}
}

func TestStateManager_RecordEscPress_TimingEdgeCases(t *testing.T) {
	sm := NewStateManager(false)

	tests := []struct {
		name          string
		delay         time.Duration
		shouldTrigger bool
	}{
		{"0ms delay", 0, true},
		{"50ms delay", 50 * time.Millisecond, true},
		{"100ms delay", 100 * time.Millisecond, true},
		{"200ms delay", 200 * time.Millisecond, true},
		{"290ms delay", 290 * time.Millisecond, true},
		{"310ms delay", 310 * time.Millisecond, false},
		{"350ms delay", 350 * time.Millisecond, false},
		{"500ms delay", 500 * time.Millisecond, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm.ResetEscTracking()

			sm.RecordEscPress()

			if tt.delay > 0 {
				time.Sleep(tt.delay)
			}

			result := sm.RecordEscPress()

			if result != tt.shouldTrigger {
				t.Errorf("With %v delay: expected trigger=%v, got %v", tt.delay, tt.shouldTrigger, result)
			}
		})
	}
}
