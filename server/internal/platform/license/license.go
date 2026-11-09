// Package license reads the signed license file of Atlas Pro (ATLAS.md §18). The file is checked offline with
// Morse's public key; nothing is sent anywhere. Without a valid license Atlas runs as Core.
package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// Plans.
const (
	PlanCore = "core"
	PlanPro  = "pro"
)

// Features a Pro license can switch on. Values match the API.
const (
	FeatureAlerts        = "alerts"
	FeatureMap           = "map"
	FeatureSSO           = "sso"
	FeatureInheritedACL  = "inherited_acl"
	FeatureAuditReports  = "audit_reports"
	FeatureSharePoint    = "sharepoint"
	FeatureERP           = "erp"
	dateLayout           = time.DateOnly
	publicKeyBytes       = ed25519.PublicKeySize
	errInvalidLicenseMsg = "the license file is not valid"
)

// License is what the file grants.
type License struct {
	Plan         string
	Organization string
	ValidUntil   time.Time
	Features     []string
}

// Has reports whether the license switches on feature.
func (l License) Has(feature string) bool {
	for _, f := range l.Features {
		if f == feature {
			return true
		}
	}
	return false
}

// Core is the license of an installation without a file.
func Core() License { return License{Plan: PlanCore, Features: []string{}} }

// file is the signed container: the payload as written, and Morse's signature over exactly those bytes.
type file struct {
	Payload   string `json:"payload"`   // base64 of the JSON below
	Signature string `json:"signature"` // base64 Ed25519
}

type payload struct {
	Plan         string   `json:"plan"`
	Organization string   `json:"organization"`
	ValidUntil   string   `json:"valid_until"`
	Features     []string `json:"features"`
}

// ErrInvalid is returned for a file that is malformed, unsigned or signed with another key.
var ErrInvalid = errors.New(errInvalidLicenseMsg)

// Parse checks the signature and returns the license. After valid_until the plan stays Pro but no feature is
// on: people see that it expired and what to renew, and nothing Pro keeps working.
func Parse(raw []byte, key ed25519.PublicKey, now time.Time) (License, error) {
	var f file
	if err := json.Unmarshal(raw, &f); err != nil {
		return License{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	body, sig, err := decode(f)
	if err != nil || len(key) != publicKeyBytes || !ed25519.Verify(key, body, sig) {
		return License{}, ErrInvalid
	}
	return fromPayload(body, now)
}

func decode(f file) (body, sig []byte, err error) {
	if body, err = base64.StdEncoding.DecodeString(f.Payload); err != nil {
		return nil, nil, err
	}
	sig, err = base64.StdEncoding.DecodeString(f.Signature)
	return body, sig, err
}

func fromPayload(body []byte, now time.Time) (License, error) {
	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		return License{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	until, err := time.Parse(dateLayout, p.ValidUntil)
	if err != nil || p.Plan != PlanPro {
		return License{}, ErrInvalid
	}
	lic := License{Plan: PlanPro, Organization: p.Organization, ValidUntil: until, Features: p.Features}
	if lic.Features == nil || now.After(until.AddDate(0, 0, 1)) {
		lic.Features = []string{}
	}
	return lic, nil
}

// Holder keeps the license current: it re-reads the file when it changes, so a renewal needs no restart.
type Holder struct {
	Path string
	Key  ed25519.PublicKey
	Now  func() time.Time

	mu      sync.Mutex
	loaded  time.Time // modification time of the file last read
	current License
}

// NewHolder returns a Holder. With an empty path it always answers Core.
func NewHolder(path string, key ed25519.PublicKey) *Holder {
	return &Holder{Path: path, Key: key, Now: time.Now, current: Core()}
}

// Current returns the license in force. A missing or invalid file means Core, never an error that stops requests.
func (h *Holder) Current() License {
	if h.Path == "" {
		return Core()
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.refresh()
	return h.withExpiry(h.current)
}

// refresh re-reads the file if it changed since the last read.
func (h *Holder) refresh() {
	info, err := os.Stat(h.Path)
	if err != nil {
		h.current, h.loaded = Core(), time.Time{}
		return
	}
	if info.ModTime().Equal(h.loaded) {
		return
	}
	raw, err := os.ReadFile(h.Path)
	if err != nil {
		h.current, h.loaded = Core(), time.Time{}
		return
	}
	lic, err := Parse(raw, h.Key, h.Now())
	if err != nil {
		lic = Core()
	}
	h.current, h.loaded = lic, info.ModTime()
}

// withExpiry switches features off once the license has run out, even if the file was read before.
func (h *Holder) withExpiry(l License) License {
	if l.Plan == PlanPro && h.Now().After(l.ValidUntil.AddDate(0, 0, 1)) {
		l.Features = []string{}
	}
	return l
}

// Features lists every feature name, for validating a file and documenting the API.
func Features() []string {
	return []string{FeatureAlerts, FeatureMap, FeatureSSO, FeatureInheritedACL, FeatureAuditReports, FeatureSharePoint, FeatureERP}
}

// Grant is what a new license file switches on.
type Grant struct {
	Organization string
	ValidUntil   time.Time
	Features     []string
}

// Sign writes a license file for grant, signed with Morse's private key. Only the tool that issues licenses
// calls it; the server never holds a private key.
func Sign(key ed25519.PrivateKey, grant Grant) ([]byte, error) {
	body, err := json.Marshal(payload{Plan: PlanPro, Organization: grant.Organization, ValidUntil: grant.ValidUntil.Format(dateLayout), Features: grant.Features})
	if err != nil {
		return nil, fmt.Errorf("license payload: %w", err)
	}
	signed := file{Payload: base64.StdEncoding.EncodeToString(body), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, body))}
	return json.Marshal(signed)
}
