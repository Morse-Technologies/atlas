package license

import (
	"crypto/ed25519"
	_ "embed" // morse_ed25519.pub
	"encoding/base64"
	"fmt"
	"strings"
)

// morsePublicKey is Morse's public key, base64. It is empty until Morse hands it over: with no key no license
// validates, so a production build runs as Core, never open to a forged file.
//
//go:embed morse_ed25519.pub
var morsePublicKey string

// PublicKey is the key that licenses are checked against: the one built into the binary. A binary built with the
// devlicense tag lets ATLAS_LICENSE_PUBLIC_KEY replace it, for development licenses; any other build ignores it.
// An empty key is not an error: nothing will verify, and the installation stays Core.
func PublicKey() (ed25519.PublicKey, error) {
	text := strings.TrimSpace(morsePublicKey)
	if override := developmentKey(); override != "" {
		text = override
	}
	if text == "" {
		return nil, nil
	}
	key, err := base64.StdEncoding.DecodeString(text)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("the license public key is not a base64 Ed25519 key")
	}
	return key, nil
}
