package documents

import "testing"

func TestHasPreviewForPDFAndRenderedFormatsOnly(t *testing.T) {
	cases := map[string]bool{
		"application/pdf": true,
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true,
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":       false,
		"image/png":    false,
		"text/unknown": false,
	}
	for mime, want := range cases {
		if got := HasPreview(mime); got != want {
			t.Errorf("HasPreview(%q) = %v, want %v", mime, got, want)
		}
	}
}
