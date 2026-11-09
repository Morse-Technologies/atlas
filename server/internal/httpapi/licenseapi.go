package httpapi

import (
	"context"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/morse/atlas/server/internal/platform/license"
)

// LicenseSource is what the HTTP layer needs from the license: the one in force now. *license.Holder implements it.
type LicenseSource interface {
	Current() license.License
}

// GetLicense tells the UI what the installation may use: the plan, who it is for, until when, and which Pro features are on.
func (s *Server) GetLicense(_ context.Context, _ GetLicenseRequestObject) (GetLicenseResponseObject, error) {
	lic := license.Core()
	if s.Licenses != nil {
		lic = s.Licenses.Current()
	}
	features := make([]LicenseFeatures, len(lic.Features))
	for i, f := range lic.Features {
		features[i] = LicenseFeatures(f)
	}
	return GetLicense200JSONResponse{Plan: LicensePlan(lic.Plan), Organization: lic.Organization,
		ValidUntil: openapi_types.Date{Time: lic.ValidUntil}, Features: features}, nil
}
