package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/morse/atlas/server/internal/platform/license"
)

// issue makes a key pair and a license valid for days, and returns the public key and the file.
func issue(t *testing.T, days string) (ed25519.PublicKey, []byte) {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "dev.lic")
	if err := run([]string{"keygen", "--dir", dir}); err != nil {
		t.Fatal(err)
	}
	args := []string{"sign", "--key", filepath.Join(dir, privateKeyFile), "--org", "Acme", "--days", days, "--features", "alerts,map", "--out", out}
	if err := run(args); err != nil {
		t.Fatal(err)
	}
	text, _ := os.ReadFile(filepath.Join(dir, publicKeyFile))
	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(text)))
	raw, readErr := os.ReadFile(out)
	if err != nil || readErr != nil {
		t.Fatal(err, readErr)
	}
	return pub, raw
}

func TestASignedLicenseIsAcceptedByTheServerCode(t *testing.T) {
	pub, raw := issue(t, "30")
	lic, err := license.Parse(raw, pub, time.Now())
	if err != nil || !lic.Has(license.FeatureAlerts) || !lic.Has(license.FeatureMap) || lic.Has(license.FeatureSSO) {
		t.Fatalf("license %+v, err %v", lic, err)
	}
}

func TestAnExpiredLicenseKeepsProButSwitchesFeaturesOff(t *testing.T) {
	pub, raw := issue(t, "-5")
	lic, err := license.Parse(raw, pub, time.Now())
	if err != nil || lic.Plan != license.PlanPro || lic.Has(license.FeatureAlerts) {
		t.Fatalf("license %+v, err %v", lic, err)
	}
}

func TestALicenseSignedWithAnotherKeyIsRefused(t *testing.T) {
	_, raw := issue(t, "30")
	other, _ := issue(t, "30")
	if _, err := license.Parse(raw, other, time.Now()); err == nil {
		t.Fatal("a license must only verify with the key that signed it")
	}
}
