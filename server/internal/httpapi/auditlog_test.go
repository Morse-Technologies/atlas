package httpapi

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/platform/signing"
)

type exportingAudit struct{ AuditLog }

func (exportingAudit) Export(_ context.Context, req audit.ExportRequest, w io.Writer) (audit.Digest, error) {
	_, err := io.WriteString(w, "id\n1\n")
	return audit.Digest{Sum: []byte("sum"), Rows: 1}, err
}

type fixedSigner struct{ SigningKeys }

func (fixedSigner) SignDigest(context.Context, uuid.UUID, []byte) (signing.Signature, error) {
	return signing.Signature{Value: []byte("signature"), KeyID: "key-1"}, nil
}

func TestExportSendsTheFileWithItsSignatureInHeaders(t *testing.T) {
	srv := &Server{Accounts: people, Licenses: allFeatures, Audit: exportingAudit{}, Signing: fixedSigner{}}
	req := adminRequest(http.MethodGet, "/api/v1/audit/export?from=2026-11-03&to=2026-11-04&format=csv", "")
	req.Header.Set("Cookie", sessionCookie+"=auditor")
	res := serveTo(t, srv, req)
	if res.Code != http.StatusOK || res.Body.String() != "id\n1\n" {
		t.Fatalf("status %d, body %q", res.Code, res.Body)
	}
	if got := res.Header().Get("Atlas-Signature"); got != base64.StdEncoding.EncodeToString([]byte("signature")) {
		t.Fatalf("Atlas-Signature = %q", got)
	}
	if res.Header().Get("Atlas-Signature-Key-Id") != "key-1" || res.Header().Get("Content-Type") != "text/csv" ||
		!strings.Contains(res.Header().Get("Content-Disposition"), "atlas-audit-2026-11-03-2026-11-04.csv") {
		t.Fatalf("headers %v", res.Header())
	}
}

func TestExportOfABackwardsRangeIsRefused(t *testing.T) {
	req := adminRequest(http.MethodGet, "/api/v1/audit/export?from=2026-11-04&to=2026-11-03", "")
	req.Header.Set("Cookie", sessionCookie+"=auditor")
	if res := serveTo(t, &Server{Accounts: people, Licenses: allFeatures}, req); res.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", res.Code)
	}
}

type verifyingAudit struct{ AuditLog }

func (verifyingAudit) Verify(context.Context, uuid.UUID) (audit.Verification, error) {
	broken := int64(7)
	return audit.Verification{OK: false, Checked: 12, BrokenAt: &broken}, nil
}

func TestVerifyAuditSaysWhereTheChainBreaks(t *testing.T) {
	server := &Server{Audit: verifyingAudit{}}

	res, err := server.VerifyAudit(personContextOf(access.Principal{}), VerifyAuditRequestObject{})

	body := res.(VerifyAudit200JSONResponse)
	if err != nil || body.Ok || body.Checked != 12 || *body.BrokenAt != 7 {
		t.Fatalf("body=%+v err=%v", body, err)
	}
}

type publishedKey struct{ SigningKeys }

func (publishedKey) PublicKey(context.Context, uuid.UUID) (signing.PublicKey, error) {
	return signing.PublicKey{ID: "key-1", Key: []byte("public")}, nil
}

func TestGetSigningKeyPublishesTheKeyInBase64(t *testing.T) {
	server := &Server{Signing: publishedKey{}}

	res, err := server.GetSigningKey(personContextOf(access.Principal{}), GetSigningKeyRequestObject{})

	body := res.(GetSigningKey200JSONResponse)
	if err != nil || body.KeyId != "key-1" || body.PublicKey != base64.StdEncoding.EncodeToString([]byte("public")) {
		t.Fatalf("body=%+v err=%v", body, err)
	}
}
