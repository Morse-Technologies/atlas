package signing

import (
	"crypto/ed25519"
	"crypto/sha512"
	"testing"

	"github.com/morse/atlas/server/internal/platform/idempotency/platformdb"
)

// plainSealer keeps the key as it is: these tests are about signatures, not about sealing.
type plainSealer struct{}

func (plainSealer) Seal(plain []byte) ([]byte, error)  { return plain, nil }
func (plainSealer) Open(sealed []byte) ([]byte, error) { return sealed, nil }

func signedFile(t *testing.T, content string) (ed25519.PublicKey, []byte) {
	t.Helper()
	keys := &Keys{Sealer: plainSealer{}}
	params, err := keys.newKey()
	if err != nil {
		t.Fatal(err)
	}
	row := platformdb.PlatformSigningKey{KeyID: params.KeyID, PublicKey: params.PublicKey, PrivateKeySealed: params.PrivateKeySealed}
	digest := sha512.Sum512([]byte(content))
	sig, err := keys.signWith(row, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return params.PublicKey, sig.Value
}

func TestASignedFileVerifies(t *testing.T) {
	public, sig := signedFile(t, "id,action\n1,user.login\n")
	digest := sha512.Sum512([]byte("id,action\n1,user.login\n"))
	if !Verify(public, digest[:], sig) {
		t.Fatal("the file that was signed must verify")
	}
}

func TestAFileChangedByOneByteDoesNotVerify(t *testing.T) {
	public, sig := signedFile(t, "id,action\n1,user.login\n")
	digest := sha512.Sum512([]byte("id,action\n2,user.login\n"))
	if Verify(public, digest[:], sig) {
		t.Fatal("a file with a byte changed must not verify")
	}
}

func TestAnotherKeyDoesNotVerify(t *testing.T) {
	_, sig := signedFile(t, "report")
	other, _ := signedFile(t, "report")
	digest := sha512.Sum512([]byte("report"))
	if Verify(other, digest[:], sig) {
		t.Fatal("a signature must verify only under the key that made it")
	}
}

func TestAKeyOfTheWrongSizeNeverVerifies(t *testing.T) {
	if Verify(ed25519.PublicKey("short"), []byte("digest"), []byte("sig")) {
		t.Fatal("a malformed key must be refused, not crash")
	}
}
