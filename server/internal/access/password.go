package access

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters (OWASP minimum profile). They are written into every hash, so they can be raised later
// without breaking existing passwords.
const (
	argonTime      = 3
	argonMemoryKiB = 64 * 1024
	argonThreads   = 2
	argonKeyBytes  = 32
	argonSaltBytes = 16
	hashFields     = 6 // "", "argon2id", "v=..", "m=..,t=..,p=..", salt, key
	fieldAlgorithm = 1
	fieldParams    = 3
	fieldSalt      = 4
	fieldKey       = 5
)

// HashPassword returns the PHC-encoded Argon2id hash of password with a fresh random salt.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyBytes)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemoryKiB, argonTime,
		argonThreads, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// VerifyPassword reports whether password matches a hash made by HashPassword. A malformed hash never matches.
func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != hashFields || parts[fieldAlgorithm] != "argon2id" {
		return false
	}
	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[fieldParams], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false
	}
	salt, errSalt := base64.RawStdEncoding.DecodeString(parts[fieldSalt])
	want, errKey := base64.RawStdEncoding.DecodeString(parts[fieldKey])
	if errSalt != nil || errKey != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}
