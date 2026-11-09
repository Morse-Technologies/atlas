// Package formats is the single list of file formats the pipeline ingests: how each is detected, whether it
// is a ZIP container (for the hostile-file checks) and whether it is rendered to PDF before parsing.
package formats

import (
	"fmt"
	"mime"

	"github.com/gabriel-vasile/mimetype"

	"github.com/morse/atlas/server/internal/platform/errs"
)

// Format is one supported content type.
type Format struct {
	MIME string
	// Ext names uploads: Gotenberg and docling pick their converter from the extension.
	Ext string
	// RenderToPDF: converted to PDF first, so there is one viewer and one way to cite. Spreadsheets are
	// parsed natively because LibreOffice pagination breaks sheets (ADR-0029).
	RenderToPDF bool
	// ZipContainer: a ZIP underneath, checked for decompression bombs (ATLAS.md §8.1).
	ZipContainer bool
}

// supported lists part 1 formats. Email arrives in part 2 with atlas-ml.
var supported = []Format{
	{MIME: "application/pdf", Ext: ".pdf"},
	{MIME: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Ext: ".docx", RenderToPDF: true, ZipContainer: true},
	{MIME: "application/vnd.openxmlformats-officedocument.presentationml.presentation", Ext: ".pptx", RenderToPDF: true, ZipContainer: true},
	{MIME: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", Ext: ".xlsx", ZipContainer: true},
	{MIME: "application/vnd.oasis.opendocument.text", Ext: ".odt", RenderToPDF: true, ZipContainer: true},
	{MIME: "application/vnd.oasis.opendocument.presentation", Ext: ".odp", RenderToPDF: true, ZipContainer: true},
	{MIME: "application/msword", Ext: ".doc", RenderToPDF: true},
	{MIME: "application/vnd.ms-powerpoint", Ext: ".ppt", RenderToPDF: true},
	{MIME: "image/png", Ext: ".png"},
	{MIME: "image/jpeg", Ext: ".jpg"},
	{MIME: "image/tiff", Ext: ".tiff"},
}

var byMIME = indexByMIME(supported)

func indexByMIME(list []Format) map[string]Format {
	out := make(map[string]Format, len(list))
	for _, f := range list {
		out[f.MIME] = f
	}
	return out
}

// ByMIME returns the format of a content type already detected and stored.
func ByMIME(contentType string) (Format, bool) {
	f, ok := byMIME[contentType]
	return f, ok
}

// Detect reads the content type from the file's bytes, never from its extension.
// An unsupported type is a permanent error with reason "unsupported".
func Detect(path string) (Format, error) {
	detected, err := mimetype.DetectFile(path)
	if err != nil {
		return Format{}, fmt.Errorf("detect type: %w", err)
	}
	base, _, err := mime.ParseMediaType(detected.String())
	if err != nil {
		return Format{}, fmt.Errorf("detect type: %w", err)
	}
	f, ok := byMIME[base]
	if !ok {
		return Format{}, errs.NewPermanent(errs.ReasonUnsupported, fmt.Errorf("content type %s is not ingested", base))
	}
	return f, nil
}

// UploadName is the file name sent to the conversion services.
func (f Format) UploadName() string {
	return "document" + f.Ext
}
