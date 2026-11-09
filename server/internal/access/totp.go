package access

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 authenticator apps use HMAC-SHA1.
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"time"
)

// TOTP as in RFC 6238 with the defaults every authenticator app supports.
const (
	totpStep        = 30 * time.Second
	totpDigits      = 1_000_000 // 10^6: six digits
	totpSecretBytes = 20
	totpSkewSteps   = 1 // accept the previous and next step for clock drift
	totpCodeLen     = 6
	truncateMask    = 0x7fffffff
	truncateOffset  = 0x0f
	truncateBytes   = 4
)

// NewTOTPSecret returns a fresh random shared secret.
func NewTOTPSecret() ([]byte, error) {
	secret := make([]byte, totpSecretBytes)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("new totp secret: %w", err)
	}
	return secret, nil
}

// VerifyTOTP reports whether code is valid for secret around now.
// shortcut: a valid code can be replayed inside its 30 s window; store the last used step if that matters.
func VerifyTOTP(secret []byte, code string, now time.Time) bool {
	if len(code) != totpCodeLen {
		return false
	}
	step := now.Unix() / int64(totpStep.Seconds())
	valid := 0
	for skew := -totpSkewSteps; skew <= totpSkewSteps; skew++ {
		want := totpCode(secret, uint64(step+int64(skew)))
		valid |= subtle.ConstantTimeCompare([]byte(code), []byte(want))
	}
	return valid == 1
}

// TOTPURI is the otpauth:// URI an authenticator app reads from a QR code.
func TOTPURI(issuer, account string, secret []byte) string {
	label := url.PathEscape(issuer + ":" + account)
	query := url.Values{"secret": {base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)}, "issuer": {issuer}}
	return "otpauth://totp/" + label + "?" + query.Encode()
}

func totpCode(secret []byte, counter uint64) string {
	mac := hmac.New(sha1.New, secret)
	_ = binary.Write(mac, binary.BigEndian, counter)
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & truncateOffset
	value := binary.BigEndian.Uint32(sum[offset:offset+truncateBytes]) & truncateMask
	return fmt.Sprintf("%0*d", totpCodeLen, value%totpDigits)
}

// CodeAt is the code an authenticator app shows at the given time. Tests and support tooling use it.
func CodeAt(secret []byte, at time.Time) string {
	return totpCode(secret, uint64(at.Unix()/int64(totpStep.Seconds())))
}
