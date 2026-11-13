// Package signing signs the files the installation exports (audit reports) with an Ed25519 key of its own, made
// the first time it is needed. Whoever holds the public key can check a file; the private half never leaves the
// server unsealed. The signature is Ed25519ph over the SHA-512 of the file, so a file is signed as it is streamed
// out, without loading it into memory.
package signing

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/platform/idempotency/platformdb"
)

// Algorithm names what the signature is, for the people and tools that verify it.
const Algorithm = "ed25519ph-sha512"

const keyIDChars = 16

var options = &ed25519.Options{Hash: crypto.SHA512}

// Sealer protects the private key at rest. *access.Box implements it.
type Sealer interface {
	Seal(plain []byte) ([]byte, error)
	Open(sealed []byte) ([]byte, error)
}

// Keys holds the signing key of each tenant.
type Keys struct {
	DB     *db.DB
	Sealer Sealer // nil: the keys can be read (public half) but not made or used
}

// PublicKey is the half a verifier needs, and the name that goes with every signature it checks.
type PublicKey struct {
	ID  string
	Key ed25519.PublicKey
}

// Signature is a signature and the id of the key that made it.
type Signature struct {
	Value []byte
	KeyID string
}

// SignDigest signs the SHA-512 of a file with the tenant's key, making the key if there is none yet.
func (k *Keys) SignDigest(ctx context.Context, tenant uuid.UUID, digest []byte) (Signature, error) {
	var sig Signature
	err := k.DB.InTenantTx(ctx, tenant, func(tx pgx.Tx) error {
		row, err := k.ensure(ctx, tx)
		if err != nil {
			return err
		}
		sig, err = k.signWith(row, digest)
		return err
	})
	return sig, err
}

func (k *Keys) signWith(row platformdb.PlatformSigningKey, digest []byte) (Signature, error) {
	seed, err := k.Sealer.Open(row.PrivateKeySealed)
	if err != nil {
		return Signature{}, fmt.Errorf("open signing key: %w", err)
	}
	value, err := ed25519.NewKeyFromSeed(seed).Sign(rand.Reader, digest, options)
	return Signature{Value: value, KeyID: row.KeyID}, err
}

// PublicKey returns the tenant's public key. With a Sealer it makes the key on first use, so an auditor can fetch
// it before the first export; without one (the console) a tenant that never signed has none: ErrNotFound.
func (k *Keys) PublicKey(ctx context.Context, tenant uuid.UUID) (PublicKey, error) {
	var out PublicKey
	err := k.DB.InTenantTx(ctx, tenant, func(tx pgx.Tx) error {
		row, err := k.load(ctx, tx)
		if err != nil {
			return err
		}
		out = PublicKey{ID: row.KeyID, Key: row.PublicKey}
		return nil
	})
	return out, err
}

func (k *Keys) load(ctx context.Context, tx pgx.Tx) (platformdb.PlatformSigningKey, error) {
	if k.Sealer != nil {
		return k.ensure(ctx, tx)
	}
	row, err := platformdb.New(tx).GetSigningKey(ctx)
	return row, db.NotFoundIfNoRows(err, "signing key")
}

// ensure returns the tenant's key, making it first if needed. Two requests racing to make it end up with one.
func (k *Keys) ensure(ctx context.Context, tx pgx.Tx) (platformdb.PlatformSigningKey, error) {
	q := platformdb.New(tx)
	row, err := q.GetSigningKey(ctx)
	if err == nil || !errors.Is(err, pgx.ErrNoRows) {
		return row, err
	}
	params, err := k.newKey()
	if err != nil {
		return row, err
	}
	if err := q.InsertSigningKey(ctx, params); err != nil {
		return row, fmt.Errorf("store signing key: %w", err)
	}
	return q.GetSigningKey(ctx)
}

func (k *Keys) newKey() (platformdb.InsertSigningKeyParams, error) {
	if k.Sealer == nil {
		return platformdb.InsertSigningKeyParams{}, fmt.Errorf("no sealer to protect a new signing key: %w", errs.ErrInvalid)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return platformdb.InsertSigningKeyParams{}, fmt.Errorf("generate signing key: %w", err)
	}
	sealed, err := k.Sealer.Seal(private.Seed())
	return platformdb.InsertSigningKeyParams{KeyID: keyIDOf(public), PublicKey: public, PrivateKeySealed: sealed}, err
}

func keyIDOf(public ed25519.PublicKey) string {
	sum := sha256.Sum256(public)
	return hex.EncodeToString(sum[:])[:keyIDChars]
}

// Verify reports whether signature is the Ed25519ph signature of digest (a SHA-512) under public.
func Verify(public ed25519.PublicKey, digest, signature []byte) bool {
	return len(public) == ed25519.PublicKeySize && ed25519.VerifyWithOptions(public, digest, signature, options) == nil
}
