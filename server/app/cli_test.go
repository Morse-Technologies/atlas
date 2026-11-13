package app

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/morse/atlas/server/internal/search"
)

func TestSingleIDArgRejectsMissingExtraAndMalformedIDs(t *testing.T) {
	for _, args := range [][]string{nil, {"a", "b"}, {"not-a-uuid"}} {
		if _, err := singleIDArg(args, "usage"); err == nil {
			t.Errorf("singleIDArg(%q) accepted bad input", args)
		}
	}
	id := uuid.New()
	if got, err := singleIDArg([]string{id.String()}, "usage"); err != nil || got != id {
		t.Fatalf("singleIDArg() = %s, %v", got, err)
	}
}

// signedReport signs report the way the server does: Ed25519ph over its SHA-512.
func signedReport(t *testing.T, report string) (ed25519.PublicKey, []byte) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha512.Sum512([]byte(report))
	signature, err := private.Sign(rand.Reader, digest[:], &ed25519.Options{Hash: crypto.SHA512})
	if err != nil {
		t.Fatal(err)
	}
	return public, signature
}

func TestAGoodReportVerifies(t *testing.T) {
	key, signature := signedReport(t, "id,at\n1,2026-11-12\n")
	if err := verifyExport(strings.NewReader("id,at\n1,2026-11-12\n"), signature, key); err != nil {
		t.Fatalf("verifyExport() = %v, want nil", err)
	}
}

func TestAReportChangedByOneByteDoesNotVerify(t *testing.T) {
	key, signature := signedReport(t, "id,at\n1,2026-11-12\n")
	err := verifyExport(strings.NewReader("id,at\n2,2026-11-12\n"), signature, key)
	if !errors.Is(err, errExportDoesNotVerify) {
		t.Fatalf("verifyExport() = %v, want errExportDoesNotVerify", err)
	}
}

func TestAReportSignedByAnotherKeyDoesNotVerify(t *testing.T) {
	_, signature := signedReport(t, "report")
	other, _ := signedReport(t, "report")
	if err := verifyExport(strings.NewReader("report"), signature, other); !errors.Is(err, errExportDoesNotVerify) {
		t.Fatalf("verifyExport() = %v, want errExportDoesNotVerify", err)
	}
}

func TestVerifyExportNeedsTheFileAndTheSignature(t *testing.T) {
	for _, args := range [][]string{nil, {"--file", "r.csv"}, {"--signature", "s"}} {
		if _, err := parseExportFiles(args); err == nil {
			t.Errorf("parseExportFiles(%q) accepted incomplete arguments", args)
		}
	}
}

func TestEvalQuestionsAreReadOnePerLine(t *testing.T) {
	file := `{"question": "How is the spindle fitted?", "expected": {"code": "PNT-014", "page": 4}}

{"question": "Who won the 1998 cup?", "expected": null}
{"question": "Where is the manual?", "expected": {"path": "docs/manual.pdf"}}
`
	cases, err := parseEvalCases(strings.NewReader(file))
	if err != nil || len(cases) != 3 {
		t.Fatalf("parseEvalCases() = %d cases, %v", len(cases), err)
	}
	if got := cases[0].Expected; got.Code != "PNT-014" || got.Page != 4 || cases[1].Expected != nil || cases[2].Expected.Path != "docs/manual.pdf" {
		t.Fatalf("cases = %+v", cases)
	}
}

func TestAnEvalFileWithAMistakeIsRefusedWithItsLine(t *testing.T) {
	tests := map[string]string{
		"not json":              "hello",
		"empty question":        `{"question": " ", "expected": null}`,
		"neither code nor path": `{"question": "q", "expected": {"page": 2}}`,
		"both code and path":    `{"question": "q", "expected": {"code": "A", "path": "b.pdf"}}`,
		"negative page":         `{"question": "q", "expected": {"code": "A", "page": -1}}`,
		"unknown field":         `{"question": "q", "expcted": null}`,
	}
	for name, line := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := parseEvalCases(strings.NewReader(`{"question": "ok", "expected": null}` + "\n" + line))
			if err == nil || !strings.Contains(err.Error(), "line 2") {
				t.Fatalf("parseEvalCases() = %v, want an error naming line 2", err)
			}
		})
	}
}

func TestAnEvalFileWithoutQuestionsIsRefused(t *testing.T) {
	if _, err := parseEvalCases(strings.NewReader("\n  \n")); !errors.Is(err, errNoQuestions) {
		t.Fatalf("parseEvalCases() = %v, want errNoQuestions", err)
	}
}

func TestEvalNeedsTheFileAndThePerson(t *testing.T) {
	for _, args := range [][]string{nil, {"--file", "q.jsonl"}, {"--as", "ana@acme.test"}} {
		if _, err := parseEvalOptions(args); err == nil {
			t.Errorf("parseEvalOptions(%q) accepted incomplete arguments", args)
		}
	}
}

func TestEvalFailsBelowTheBarAndPassesAtIt(t *testing.T) {
	good := search.EvalReport{Precision: search.MinPrecision}
	bad := search.EvalReport{Precision: search.MinPrecision - 0.1}
	if err := belowTheBar(good); err != nil {
		t.Errorf("belowTheBar(good) = %v, want nil", err)
	}
	if err := belowTheBar(bad); err == nil {
		t.Error("belowTheBar(bad) = nil, want an error (exit status 1)")
	}
}
