package httpapi

import (
	"context"
	"fmt"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/platform/license"
)

// LicenseSource is what the HTTP layer needs from the license: the one in force now, and a way to replace it.
// *license.Holder implements it.
type LicenseSource interface {
	Current() license.License
	Install(raw []byte) (license.License, error)
}

// licenseSubject names the license in audit events: there is one, so it needs no id.
const licenseSubject = "license:current"

// GetLicense tells the UI what the installation may use: the plan, who it is for, until when, and which Pro features are on.
func (s *Server) GetLicense(_ context.Context, _ GetLicenseRequestObject) (GetLicenseResponseObject, error) {
	lic := license.Core()
	if s.Licenses != nil {
		lic = s.Licenses.Current()
	}
	return GetLicense200JSONResponse(toLicenseJSON(lic)), nil
}

// InstallLicense checks and installs a license file and records the outcome. A refusal is audited with its fixed
// reason, never with the content of the file.
func (s *Server) InstallLicense(ctx context.Context, req InstallLicenseRequestObject) (InstallLicenseResponseObject, error) {
	if s.Licenses == nil {
		return nil, fmt.Errorf("this server has no license holder: %w", errs.ErrConflict)
	}
	lic, err := s.Licenses.Install([]byte(req.Body.Content))
	if auditErr := s.recordLicense(ctx, lic, err); auditErr != nil {
		return nil, auditErr
	}
	if err != nil {
		return nil, err
	}
	return InstallLicense200JSONResponse(toLicenseJSON(lic)), nil
}

// recordLicense writes license.installed, or license.rejected when the file was refused. A failure of the server
// itself (nothing was decided about the file) is not an event.
func (s *Server) recordLicense(ctx context.Context, lic license.License, installErr error) error {
	entry := audit.Entry{At: time.Now(), Actor: principalFrom(ctx).UserID, Action: "license.installed", Subject: licenseSubject,
		Detail: "valid_until=" + lic.ValidUntil.Format(time.DateOnly)}
	if installErr != nil {
		reason := license.ReasonOf(installErr)
		if reason == "" {
			return nil
		}
		entry.Action, entry.Detail = "license.rejected", "reason="+string(reason)
	}
	return s.Audit.Append(ctx, principalFrom(ctx).TenantID, entry)
}

func toLicenseJSON(lic license.License) License {
	features := make([]LicenseFeatures, len(lic.Features))
	for i, f := range lic.Features {
		features[i] = LicenseFeatures(f)
	}
	return License{Plan: LicensePlan(lic.Plan), Organization: lic.Organization,
		ValidUntil: openapi_types.Date{Time: lic.ValidUntil}, Features: features}
}
