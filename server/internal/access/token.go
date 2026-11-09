package access

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

const tokenBytes = 32

// newToken returns a random secret for a cookie or a challenge, and the hash that is stored instead of it.
func newToken() (secret string, hash []byte, err error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("new token: %w", err)
	}
	secret = base64.RawURLEncoding.EncodeToString(raw)
	return secret, hashToken(secret), nil
}

// hashToken is SHA-256: the token is already 256 random bits, so a slow hash adds nothing.
func hashToken(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}
