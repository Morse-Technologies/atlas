// Code below answers 501 for the operations later parts of the plan implement. Keep one line per
// operation: the compiler flags any operation added to api/openapi.yaml and missing here.

package httpapi

import "context"

func (s *Server) ExportAudit(context.Context, ExportAuditRequestObject) (ExportAuditResponseObject, error) {
	return nil, ErrNotImplemented
}
