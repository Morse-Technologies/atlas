//go:build !devlicense

package license

import "testing"

func TestTheEnvironmentCannotReplaceMorsesKeyInANormalBuild(t *testing.T) {
	t.Setenv("ATLAS_LICENSE_PUBLIC_KEY", "AAAA")
	key, err := PublicKey()
	if err != nil || key != nil {
		t.Fatalf("PublicKey = %x, %v; a normal build ignores the variable (and the embedded key is empty until Morse's arrives)", key, err)
	}
}
