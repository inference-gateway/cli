package utils

import (
	"crypto/rand"
	"encoding/hex"
)

// RandomToken is 24 crypto-random bytes hex encoded: an opaque token for a
// service binding.
func RandomToken() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
