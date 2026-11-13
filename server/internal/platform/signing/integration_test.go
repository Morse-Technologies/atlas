//go:build integration

package signing

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha512"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/platform/dbtest"
	"github.com/morse/atlas/server/internal/platform/errs"
)

// xorSealer hides the key from a plain reading of the table, as the real sealer does (with encryption).
type xorSealer struct{}

func (xorSealer) Seal(plain []byte) ([]byte, error)  { return xor(plain), nil }
func (xorSealer) Open(sealed []byte) ([]byte, error) { return xor(sealed), nil }

func xor(in []byte) []byte {
	out := make([]byte, len(in))
	for i, b := range in {
		out[i] = b ^ 0xAA
	}
	return out
}

func TestTheKeyIsMadeOnceAndItsPrivateHalfIsNotInTheClear(t *testing.T) {
	ctx := context.Background()
	d := dbtest.Start(t)
	tenant := dbtest.NewTenant(t, d)
	keys := &Keys{DB: d, Sealer: xorSealer{}}
	digest := sha512.Sum512([]byte("report"))

	first, err := keys.SignDigest(ctx, tenant, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	second, err := keys.SignDigest(ctx, tenant, digest[:])
	if err != nil || second.KeyID != first.KeyID {
		t.Fatalf("second key %q, %v; the key is made once", second.KeyID, err)
	}
	public, err := keys.PublicKey(ctx, tenant)
	if err != nil || !Verify(public.Key, digest[:], first.Value) {
		t.Fatalf("PublicKey = %+v, %v; it must check the signature", public, err)
	}
	var sealed []byte
	err = d.InTenantTx(ctx, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT private_key_sealed FROM platform.signing_keys").Scan(&sealed)
	})
	seed := xor(sealed) // what the sealer protects
	if err != nil || bytes.Equal(sealed, seed) || !bytes.Equal(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey), public.Key) {
		t.Fatalf("sealed %x, %v; the table must hold the seed sealed, and it must be the private half of the public key", sealed, err)
	}
}

func TestWithoutASealerAKeyIsReadButNeverMade(t *testing.T) {
	d := dbtest.Start(t)
	tenant := dbtest.NewTenant(t, d)
	if _, err := (&Keys{DB: d}).PublicKey(context.Background(), tenant); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("PublicKey without a key or a sealer = %v, want ErrNotFound", err)
	}
}

func TestAnotherTenantHasItsOwnKey(t *testing.T) {
	ctx := context.Background()
	d := dbtest.Start(t)
	keys := &Keys{DB: d, Sealer: xorSealer{}}
	a, err := keys.PublicKey(ctx, dbtest.NewTenant(t, d))
	if err != nil {
		t.Fatal(err)
	}
	b, err := keys.PublicKey(ctx, dbtest.NewTenant(t, d))
	if err != nil || a.ID == b.ID {
		t.Fatalf("keys %q and %q, %v; each tenant signs with its own", a.ID, b.ID, err)
	}
}
