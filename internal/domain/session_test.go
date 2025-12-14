package domain

import (
	"testing"
	"time"
)

func TestGenerateSessionID(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "generate unique session ID"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := GenerateSessionID()

			if id == "" {
				t.Error("GenerateSessionID() returned empty string")
			}

			if len(id) < 10 {
				t.Errorf("GenerateSessionID() = %v, expected length >= 10", id)
			}

			if id.String() != string(id) {
				t.Errorf("SessionID.String() = %v, want %v", id.String(), string(id))
			}
		})
	}
}

func TestSessionID_Uniqueness(t *testing.T) {
	ids := make(map[SessionID]bool)
	count := 100

	for i := 0; i < count; i++ {
		id := GenerateSessionID()
		if ids[id] {
			t.Errorf("Duplicate session ID generated: %v", id)
		}
		ids[id] = true
	}

	if len(ids) != count {
		t.Errorf("Expected %d unique IDs, got %d", count, len(ids))
	}
}

func TestSessionID_Timestamp(t *testing.T) {
	tests := []struct {
		name      string
		sessionID SessionID
		wantZero  bool
	}{
		{
			name:      "valid session ID with hyphen",
			sessionID: "1733678400-a3f2bc8d",
			wantZero:  false,
		},
		{
			name:      "valid session ID without hyphen",
			sessionID: "1733678400",
			wantZero:  false,
		},
		{
			name:      "invalid session ID",
			sessionID: "invalid-id",
			wantZero:  true,
		},
		{
			name:      "empty session ID",
			sessionID: "",
			wantZero:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			timestamp := tt.sessionID.Timestamp()

			if tt.wantZero {
				if timestamp != 0 {
					t.Errorf("SessionID.Timestamp() = %v, want 0", timestamp)
				}
			} else {
				if timestamp <= 0 {
					t.Errorf("SessionID.Timestamp() = %v, want > 0", timestamp)
				}
			}
		})
	}
}

func TestSessionID_Timestamp_Recent(t *testing.T) {
	id := GenerateSessionID()
	timestamp := id.Timestamp()

	now := time.Now().Unix()
	if timestamp < now-1 || timestamp > now+1 {
		t.Errorf("SessionID.Timestamp() = %v, expected to be close to %v", timestamp, now)
	}
}

func TestSessionID_Age(t *testing.T) {
	tests := []struct {
		name      string
		sessionID SessionID
		wantZero  bool
	}{
		{
			name:      "valid session ID with hyphen",
			sessionID: "1733678400-a3f2bc8d",
			wantZero:  false,
		},
		{
			name:      "invalid session ID",
			sessionID: "invalid-id",
			wantZero:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			age := tt.sessionID.Age()

			if tt.wantZero {
				if age != 0 {
					t.Errorf("SessionID.Age() = %v, want 0", age)
				}
			} else {
				if age <= 0 {
					t.Errorf("SessionID.Age() = %v, want > 0", age)
				}
			}
		})
	}
}

func TestSessionID_Age_Recent(t *testing.T) {
	id := GenerateSessionID()
	time.Sleep(10 * time.Millisecond)
	age := id.Age()

	if age < 10*time.Millisecond || age > 1*time.Second {
		t.Errorf("SessionID.Age() = %v, expected to be between 10ms and 1s", age)
	}
}

func TestSessionID_Format(t *testing.T) {
	id := GenerateSessionID()
	idStr := id.String()

	hyphenCount := 0
	for _, ch := range idStr {
		if ch == '-' {
			hyphenCount++
		}
	}

	if hyphenCount != 1 {
		t.Errorf("SessionID format incorrect, expected 1 hyphen, got %d in %v", hyphenCount, idStr)
	}

	timestamp := id.Timestamp()
	if timestamp == 0 {
		t.Errorf("SessionID timestamp extraction failed for %v", idStr)
	}
}

func TestSessionID_String(t *testing.T) {
	testID := SessionID("1733678400-a3f2bc8d")
	if testID.String() != "1733678400-a3f2bc8d" {
		t.Errorf("SessionID.String() = %v, want 1733678400-a3f2bc8d", testID.String())
	}
}
