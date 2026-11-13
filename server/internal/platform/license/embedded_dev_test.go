//go:build devlicense

package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"
)

func TestADevelopmentBuildTakesTheKeyFromTheEnvironment(t *testing.T) {
	pub, _ := keys(t)
	t.Setenv("ATLAS_LICENSE_PUBLIC_KEY", base64.StdEncoding.EncodeToString(pub))
	key, err := PublicKey()
	if err != nil || !ed25519.PublicKey(key).Equal(pub) {
		t.Fatalf("PublicKey = %x, %v; want the key of the environment", key, err)
	}
}

func TestADevelopmentBuildRefusesAMalformedKey(t *testing.T) {
	t.Setenv("ATLAS_LICENSE_PUBLIC_KEY", "not-a-key")
	if _, err := PublicKey(); err == nil {
		t.Fatal("a malformed key must be an error, not a silent Core")
	}
}
