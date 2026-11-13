package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/morse/atlas/server/internal/platform/errs"
)

var today = time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)

func signed(t *testing.T, key ed25519.PrivateKey, p payload) []byte {
	t.Helper()
	body, _ := json.Marshal(p)
	raw, _ := json.Marshal(file{Payload: base64.StdEncoding.EncodeToString(body), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, body))})
	return raw
}

func keys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

var pro = payload{Plan: "pro", Organization: "Acme", ValidUntil: "2027-01-31", Features: []string{"alerts", "map"}}

func TestAValidLicenseGrantsItsFeatures(t *testing.T) {
	pub, priv := keys(t)
	lic, err := Parse(signed(t, priv, pro), pub, today)
	if err != nil || lic.Plan != PlanPro || !lic.Has("alerts") || lic.Has("sso") || lic.Organization != "Acme" {
		t.Fatalf("license %+v, err %v", lic, err)
	}
}

func TestALicenseSignedWithAnotherKeyIsRefused(t *testing.T) {
	pub, _ := keys(t)
	_, other := keys(t)
	if _, err := Parse(signed(t, other, pro), pub, today); err == nil {
		t.Fatal("a license signed by someone else must be refused")
	}
}

func TestEditingThePayloadBreaksTheSignature(t *testing.T) {
	pub, priv := keys(t)
	raw := signed(t, priv, pro)
	var f file
	_ = json.Unmarshal(raw, &f)
	body, _ := base64.StdEncoding.DecodeString(f.Payload)
	f.Payload = base64.StdEncoding.EncodeToString([]byte(string(body[:len(body)-2]) + `,"x":1}`))
	tampered, _ := json.Marshal(f)
	if _, err := Parse(tampered, pub, today); err == nil {
		t.Fatal("a changed payload must not verify")
	}
}

func TestAnExpiredLicenseKeepsThePlanButLosesEveryFeature(t *testing.T) {
	pub, priv := keys(t)
	lic, err := Parse(signed(t, priv, pro), pub, time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || lic.Plan != PlanPro || len(lic.Features) != 0 {
		t.Fatalf("license %+v, err %v", lic, err)
	}
}

func TestTheLastDayOfTheLicenseStillCounts(t *testing.T) {
	pub, priv := keys(t)
	lic, _ := Parse(signed(t, priv, pro), pub, time.Date(2027, 1, 31, 15, 0, 0, 0, time.UTC))
	if !lic.Has("alerts") {
		t.Fatal("valid_until is inclusive")
	}
}

func TestGarbageIsRefused(t *testing.T) {
	pub, _ := keys(t)
	for _, raw := range []string{"", "not json", `{"payload":"!!","signature":"!!"}`} {
		if _, err := Parse([]byte(raw), pub, today); err == nil {
			t.Errorf("%q must be refused", raw)
		}
	}
}

func TestHolderFollowsTheFileAndFallsBackToCore(t *testing.T) {
	pub, priv := keys(t)
	path := filepath.Join(t.TempDir(), "atlas.lic")
	h := NewHolder(path, pub)
	h.Now = func() time.Time { return today }
	if h.Current().Plan != PlanCore {
		t.Fatal("no file: Core")
	}
	_ = os.WriteFile(path, signed(t, priv, pro), 0o600)
	if !h.Current().Has("map") {
		t.Fatal("a license dropped in place must take effect without a restart")
	}
	_ = os.WriteFile(path, []byte("corrupt"), 0o600)
	later := time.Now().Add(time.Hour)
	_ = os.Chtimes(path, later, later)
	if h.Current().Plan != PlanCore {
		t.Fatal("a corrupt file must fall back to Core")
	}
}

func TestHolderTurnsFeaturesOffWhenTheDatePassesWithoutAFileChange(t *testing.T) {
	pub, priv := keys(t)
	path := filepath.Join(t.TempDir(), "atlas.lic")
	_ = os.WriteFile(path, signed(t, priv, pro), 0o600)
	h := NewHolder(path, pub)
	h.Now = func() time.Time { return today }
	if !h.Current().Has("alerts") {
		t.Fatal("valid before the date")
	}
	h.Now = func() time.Time { return time.Date(2027, 2, 5, 0, 0, 0, 0, time.UTC) }
	if h.Current().Has("alerts") {
		t.Fatal("must expire on its own")
	}
}

func holderFor(t *testing.T, pub ed25519.PublicKey) (*Holder, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "license", "atlas-license.json")
	h := NewHolder(path, pub)
	h.Now = func() time.Time { return today }
	return h, path
}

func TestInstallKeepsAValidLicenseWhereEveryProcessReadsIt(t *testing.T) {
	pub, priv := keys(t)
	h, path := holderFor(t, pub)
	lic, err := h.Install(signed(t, priv, pro))
	if err != nil || !lic.Has("map") || !h.Current().Has("map") {
		t.Fatalf("Install = %+v, %v; the new license must be in force at once", lic, err)
	}
	if other := NewHolder(path, pub); !other.Current().Has("map") {
		t.Fatal("another process reading the same file must see the license")
	}
}

// badFile is a file Install must refuse, and the reason it must give.
type badFile struct {
	name   string
	raw    []byte
	reason Reason
}

func badFiles(t *testing.T, issuer, stranger ed25519.PrivateKey) []badFile {
	t.Helper()
	expired := payload{Plan: "pro", Organization: "Acme", ValidUntil: "2026-01-31", Features: []string{"alerts"}}
	return []badFile{
		{"not a license", []byte("not json"), ReasonFormat},
		{"signed by someone else", signed(t, stranger, pro), ReasonSignature},
		{"expired", signed(t, issuer, expired), ReasonExpired},
	}
}

func TestInstallRefusesBadFilesWithAFixedReasonAndKeepsTheCurrentLicense(t *testing.T) {
	pub, priv := keys(t)
	_, stranger := keys(t)
	for _, c := range badFiles(t, priv, stranger) {
		h, _ := holderFor(t, pub)
		if _, err := h.Install(signed(t, priv, pro)); err != nil {
			t.Fatal(err)
		}
		_, err := h.Install(c.raw)
		if ReasonOf(err) != c.reason || !errors.Is(err, errs.ErrInvalid) || errs.PublicDetail(err) != reasonText[c.reason] {
			t.Errorf("%s: err %v, detail %q; want reason %q", c.name, err, errs.PublicDetail(err), c.reason)
		}
		if !h.Current().Has("map") {
			t.Errorf("%s: a refused file must leave the current license in force", c.name)
		}
	}
}

func TestInstallLeavesNoTemporaryFileBehind(t *testing.T) {
	pub, priv := keys(t)
	h, path := holderFor(t, pub)
	if _, err := h.Install(signed(t, priv, pro)); err != nil {
		t.Fatal(err)
	}
	if left, _ := os.ReadDir(filepath.Dir(path)); len(left) != 1 {
		t.Fatalf("%d files in the license folder, want only the license", len(left))
	}
}

func TestWithoutAKeyNoLicenseVerifies(t *testing.T) {
	_, priv := keys(t)
	h, _ := holderFor(t, nil)
	if _, err := h.Install(signed(t, priv, pro)); ReasonOf(err) != ReasonSignature {
		t.Fatalf("err = %v; with an empty public key every license is refused", err)
	}
}
