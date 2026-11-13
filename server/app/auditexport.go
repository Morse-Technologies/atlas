package app

import (
	"context"
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/platform/signing"
)

const (
	exportVerifiedAction  = "audit.export_verified"
	exportVerifiedSubject = "audit:log"
)

// errExportDoesNotVerify says the file is not what was signed: a byte changed, or it is another key's report.
var errExportDoesNotVerify = errors.New("the signature does not match this file: it was changed or signed by another key")

// exportFiles are the paths `atlas audit verify-export` reads. PublicKey is optional: without it the installation's own key is used.
type exportFiles struct{ file, signature, publicKey string }

// auditVerifyExport: atlas audit verify-export --file F --signature S [--public-key K]. It exits with an error if the
// file does not verify, and records the check in the audit log either way.
func auditVerifyExport(ctx context.Context, s cliSession, args []string) error {
	files, err := parseExportFiles(args)
	if err != nil {
		return err
	}
	key, err := publicKeyFor(ctx, s, files.publicKey)
	if err != nil {
		return err
	}
	verifyErr := verifyExportFile(files, key)
	if err := recordVerification(ctx, s, verifyErr); err != nil {
		return err
	}
	if verifyErr == nil {
		fmt.Println("the report verifies: it is exactly what this installation signed")
	}
	return verifyErr
}

func parseExportFiles(args []string) (exportFiles, error) {
	flags := flag.NewFlagSet("audit verify-export", flag.ContinueOnError)
	file := flags.String("file", "", "the exported report")
	signature := flags.String("signature", "", "its signature (the Atlas-Signature value, as text)")
	publicKey := flags.String("public-key", "", "a file with the public key (base64); default: this installation's")
	if err := flags.Parse(args); err != nil || *file == "" || *signature == "" {
		return exportFiles{}, errors.New("usage: atlas audit verify-export --file REPORT --signature SIGNATURE [--public-key KEYFILE]")
	}
	return exportFiles{file: *file, signature: *signature, publicKey: *publicKey}, nil
}

func publicKeyFor(ctx context.Context, s cliSession, path string) (ed25519.PublicKey, error) {
	if path == "" {
		key, err := (&signing.Keys{DB: s.db}).PublicKey(ctx, s.tenantID)
		return key.Key, err
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read public key: %w", err)
	}
	return decodeBase64(string(text), "public key")
}

func decodeBase64(text, what string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
	if err != nil {
		return nil, fmt.Errorf("the %s is not base64: %w", what, err)
	}
	return raw, nil
}

func verifyExportFile(files exportFiles, key ed25519.PublicKey) error {
	text, err := os.ReadFile(files.signature)
	if err != nil {
		return fmt.Errorf("read signature: %w", err)
	}
	signature, err := decodeBase64(string(text), "signature")
	if err != nil {
		return err
	}
	report, err := os.Open(files.file)
	if err != nil {
		return fmt.Errorf("read report: %w", err)
	}
	defer func() { _ = report.Close() }()
	return verifyExport(report, signature, key)
}

// verifyExport checks that signature is the key's Ed25519ph signature of the report's SHA-512.
func verifyExport(report io.Reader, signature []byte, key ed25519.PublicKey) error {
	hash := sha512.New()
	if _, err := io.Copy(hash, report); err != nil {
		return fmt.Errorf("read report: %w", err)
	}
	if !signing.Verify(key, hash.Sum(nil), signature) {
		return errExportDoesNotVerify
	}
	return nil
}

func recordVerification(ctx context.Context, s cliSession, verifyErr error) error {
	result := "valid"
	if verifyErr != nil {
		result = "invalid"
	}
	entry := audit.Entry{At: time.Now(), Action: exportVerifiedAction, Subject: exportVerifiedSubject, Detail: "result=" + result + ", from the server console"}
	return (&audit.Service{DB: s.db}).Append(ctx, s.tenantID, entry)
}
