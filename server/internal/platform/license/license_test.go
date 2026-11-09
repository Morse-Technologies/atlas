package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
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
