package domain

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"
)

// SessionID represents a unique identifier for a chat session.
// Format: {unix-timestamp}-{8-char-random-hex}
// Example: 1733678400-a3f2bc8d
type SessionID string

// GenerateSessionID creates a new unique session identifier.
// The ID combines a Unix timestamp (for temporal uniqueness) with
// random hex characters (for collision resistance).
func GenerateSessionID() SessionID {
	timestamp := time.Now().Unix()

	randomBytes := make([]byte, 4)
	if _, err := rand.Read(randomBytes); err != nil {
		return SessionID(fmt.Sprintf("%d", timestamp))
	}

	randomHex := hex.EncodeToString(randomBytes)
	return SessionID(fmt.Sprintf("%d-%s", timestamp, randomHex))
}

// String returns the string representation of the SessionID.
func (s SessionID) String() string {
	return string(s)
}

// Timestamp extracts the Unix timestamp from the session ID.
// Returns 0 if the session ID format is invalid.
func (s SessionID) Timestamp() int64 {
	for i := 0; i < len(s); i++ {
		if s[i] == '-' {
			timestamp, err := strconv.ParseInt(string(s[:i]), 10, 64)
			if err != nil {
				return 0
			}
			return timestamp
		}
	}

	timestamp, err := strconv.ParseInt(string(s), 10, 64)
	if err != nil {
		return 0
	}
	return timestamp
}

// Age returns the duration since the session was created.
// Returns 0 if the session ID format is invalid.
func (s SessionID) Age() time.Duration {
	timestamp := s.Timestamp()
	if timestamp == 0 {
		return 0
	}
	return time.Since(time.Unix(timestamp, 0))
}
