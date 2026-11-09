package access

import (
	"strings"
	"testing"
	"time"
)

// RFC 6238 appendix B: SHA-1 secret "12345678901234567890", 8 digits there, so compare the last 6.
func TestTOTPMatchesRFC6238Vectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	cases := map[int64]string{59: "287082", 1111111109: "081804", 2000000000: "279037"}
	for unix, want := range cases {
		if got := totpCode(secret, uint64(unix/30)); got != want {
			t.Errorf("code at %d = %s, want %s", unix, got, want)
		}
	}
}

func TestVerifyTOTPAcceptsOneStepOfDriftNotTwo(t *testing.T) {
	secret := []byte("12345678901234567890")
	now := time.Unix(59, 0)
	code := totpCode(secret, 1) // step of t=59
	for skew, want := range map[time.Duration]bool{0: true, 30 * time.Second: true, 60 * time.Second: false} {
		if got := VerifyTOTP(secret, code, now.Add(skew)); got != want {
			t.Errorf("skew %v: got %v, want %v", skew, got, want)
		}
	}
}

func TestVerifyTOTPRejectsWrongLength(t *testing.T) {
	if VerifyTOTP([]byte("12345678901234567890"), "1234567", time.Unix(59, 0)) {
		t.Fatal("a 7-digit code must not pass")
	}
}

func TestPasswordHashVerifiesOnlyTheRightPassword(t *testing.T) {
	hash, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(hash, "correct horse battery") || VerifyPassword(hash, "wrong") {
		t.Fatal("hash must accept the password and nothing else")
	}
	if VerifyPassword("garbage", "x") || VerifyPassword(strings.Repeat("$", 5), "x") {
		t.Fatal("a malformed hash must never match")
	}
}

func TestBoxRoundTripAndTamper(t *testing.T) {
	box, err := NewBox(make([]byte, keyBytes))
	if err != nil {
		t.Fatal(err)
	}
	sealed, _ := box.Seal([]byte("seed"))
	if plain, err := box.Open(sealed); err != nil || string(plain) != "seed" {
		t.Fatalf("Open = %q, %v", plain, err)
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := box.Open(sealed); err == nil {
		t.Fatal("a tampered value must not open")
	}
}

func TestParseKeyRequires32Bytes(t *testing.T) {
	if _, err := ParseKey("c2hvcnQ="); err == nil {
		t.Fatal("a short key must be refused")
	}
}
