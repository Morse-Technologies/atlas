//go:build integration

package app

import (
	"context"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/platform/signing"
)

// reportSignedHere writes a report and the signature this installation gives it, and returns their paths.
func reportSignedHere(t *testing.T, s cliSession, sealer signing.Sealer) exportFiles {
	t.Helper()
	dir := t.TempDir()
	report := []byte("id,at,action\n1,2026-11-12,user.login\n")
	digest := sha512.Sum512(report)
	signature, err := (&signing.Keys{DB: s.db, Sealer: sealer}).SignDigest(context.Background(), s.tenantID, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	files := exportFiles{file: filepath.Join(dir, "report.csv"), signature: filepath.Join(dir, "report.sig")}
	if err := os.WriteFile(files.file, report, 0o600); err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(signature.Value)
	if err := os.WriteFile(files.signature, []byte(encoded), 0o600); err != nil {
		t.Fatal(err)
	}
	return files
}

// lastVerification is the detail of the newest audit.export_verified event.
func lastVerification(t *testing.T, s cliSession) string {
	t.Helper()
	events, _, err := (&audit.Service{DB: s.db}).List(context.Background(), s.tenantID, audit.Page{Size: 1})
	if err != nil || len(events) != 1 || events[0].Action != "audit.export_verified" {
		t.Fatalf("newest audit event = %+v, %v; want audit.export_verified", events, err)
	}
	return events[0].Detail
}

func TestVerifyingAnExportIsRecordedWhetherItVerifiesOrNot(t *testing.T) {
	session, svc := consoleFixture(t)
	files := reportSignedHere(t, session, svc.Box)
	args := []string{"--file", files.file, "--signature", files.signature}

	if err := auditVerifyExport(context.Background(), session, args); err != nil {
		t.Fatalf("a report this installation signed must verify: %v", err)
	}
	if detail := lastVerification(t, session); !strings.Contains(detail, "result=valid") {
		t.Errorf("detail %q, want result=valid", detail)
	}
	if err := os.WriteFile(files.file, []byte("id,at,action\n1,2026-11-12,user.deleted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := auditVerifyExport(context.Background(), session, args); !errors.Is(err, errExportDoesNotVerify) {
		t.Fatalf("a changed report = %v, want errExportDoesNotVerify", err)
	}
	if detail := lastVerification(t, session); !strings.Contains(detail, "result=invalid") {
		t.Errorf("detail %q, want result=invalid", detail)
	}
}
