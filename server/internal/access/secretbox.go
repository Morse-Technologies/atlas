package access

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

const keyBytes = 32 // AES-256

// Box encrypts small secrets (TOTP seeds) with AES-256-GCM. The key comes from ATLAS_SECRET_KEY.
type Box struct{ aead cipher.AEAD }

// ParseKey decodes the base64 form of the secret key.
func ParseKey(encoded string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != keyBytes {
		return nil, errors.New("secret key must be 32 bytes, base64 encoded")
	}
	return key, nil
}

// NewBox builds a Box from a 32-byte key.
func NewBox(key []byte) (*Box, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new box: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new box: %w", err)
	}
	return &Box{aead: aead}, nil
}

// Seal returns nonce || ciphertext.
func (b *Box) Seal(plain []byte) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("seal: %w", err)
	}
	return b.aead.Seal(nonce, nonce, plain, nil), nil
}

// Open reverses Seal. A wrong key or a tampered value fails.
func (b *Box) Open(sealed []byte) ([]byte, error) {
	size := b.aead.NonceSize()
	if len(sealed) < size {
		return nil, errors.New("open: value too short")
	}
	plain, err := b.aead.Open(nil, sealed[:size], sealed[size:], nil)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	return plain, nil
}
