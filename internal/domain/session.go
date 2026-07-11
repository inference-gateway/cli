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

// FormatChannelSessionID builds the session ID the channels-manager uses for
// a channel/sender pair, the inverse of ParseChannelSessionID.
// ponytail: channel names must not contain '-' (they're a fixed enum today;
// sender IDs may contain dashes and are parsed back as the tail).
func FormatChannelSessionID(channel, senderID string) string {
	return "channel-" + channel + "-" + senderID
}

// ParseChannelSessionID extracts the channel name and recipient ID from a
// session ID created by the channels-manager. The channel manager builds
// session IDs as "channel-<name>-<sender_id>" (see channel_manager.go).
//
// Returns ok=false when the session ID does not match this format (e.g. for
// chat-mode or generic agent sessions). Channel names cannot contain a '-';
// recipient IDs may.
func ParseChannelSessionID(sessionID string) (channel, recipientID string, ok bool) {
	const prefix = "channel-"
	if len(sessionID) <= len(prefix) || sessionID[:len(prefix)] != prefix {
		return "", "", false
	}
	rest := sessionID[len(prefix):]
	dash := -1
	for i := 0; i < len(rest); i++ {
		if rest[i] == '-' {
			dash = i
			break
		}
	}
	if dash <= 0 || dash == len(rest)-1 {
		return "", "", false
	}
	return rest[:dash], rest[dash+1:], true
}
