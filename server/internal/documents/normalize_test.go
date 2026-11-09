package documents

import (
	"os"
	"reflect"
	"testing"

	"github.com/morse/atlas/server/internal/platform/errs"
)

var wantSmallBlocks = []Block{
	{Kind: KindHeading, Page: 1, Box: &Rect{X: 50, Y: 42, W: 250, H: 20}, Text: "Montaje del husillo"},
	{Kind: KindStep, Page: 1, Box: &Rect{X: 60, Y: 82, W: 340, H: 15}, Text: "Aflojar la tuerca."},
	{Kind: KindStep, Text: "Retirar el husillo."},
	{Kind: KindTable, Page: 1, Box: &Rect{X: 50, Y: 100, W: 450, H: 100}, Text: "Elemento | Par\nTuerca | 40 Nm"},
	{Kind: KindWarning, Text: "ADVERTENCIA: bloquear la energía antes de abrir."},
}

func TestNormalizeFollowsReadingOrder(t *testing.T) {
	raw, err := os.ReadFile("testdata/docling_small.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Normalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Pages != 1 || !reflect.DeepEqual(got.Blocks, wantSmallBlocks) {
		t.Fatalf("Normalize() pages=%d blocks=%+v", got.Pages, got.Blocks)
	}
}

func TestNormalizeRejectsGarbageAsCorrupt(t *testing.T) {
	_, err := Normalize([]byte("not json"))
	if errs.PermanentReason(err) != errs.ReasonCorrupt {
		t.Fatalf("got %v, want permanent corrupt", err)
	}
}

func TestTitleFromPath(t *testing.T) {
	if got := titleFromPath("Calidad/PNT/PNT-014_v3.pdf"); got != "PNT-014_v3" {
		t.Fatalf("titleFromPath() = %q", got)
	}
}

// Edge cases Docling produces: unknown labels, dangling refs and nested children.
const edgeDocling = `{
  "body": {"children": [{"$ref": "#/texts/0"}, {"$ref": "#/texts/9"}, {"$ref": "#/nope"}, {"$ref": "#/texts/1"}]},
  "texts": [
    {"label": "brand_new_label", "text": "kept as paragraph"},
    {"label": "list_item", "text": "parent", "children": [{"$ref": "#/texts/2"}]},
    {"label": "list_item", "text": "  child  "}
  ]
}`

var wantEdgeBlocks = []Block{
	{Kind: KindParagraph, Text: "kept as paragraph"},
	{Kind: KindListItem, Text: "parent"},
	{Kind: KindListItem, Text: "child"},
}

func TestNormalizeKeepsUnknownLabelsAndSkipsDanglingRefs(t *testing.T) {
	got, err := Normalize([]byte(edgeDocling))
	if err != nil || !reflect.DeepEqual(got.Blocks, wantEdgeBlocks) {
		t.Fatalf("Normalize() = %+v, %v", got.Blocks, err)
	}
}

func TestNormalizeSurvivesACycle(t *testing.T) {
	cyclic := `{"body": {"children": [{"$ref": "#/groups/0"}]},
	  "groups": [{"children": [{"$ref": "#/groups/0"}, {"$ref": "#/texts/0"}]}],
	  "texts": [{"label": "text", "text": "once", "children": [{"$ref": "#/texts/0"}]}]}`
	got, err := Normalize([]byte(cyclic))
	if err != nil || len(got.Blocks) != 1 {
		t.Fatalf("Normalize() = %+v, %v", got.Blocks, err)
	}
}

func TestNormalizeSkipsNegativeRefs(t *testing.T) {
	negative := `{"body": {"children": [{"$ref": "#/texts/-1"}, {"$ref": "#/texts/0"}]},
	  "texts": [{"label": "text", "text": "kept"}]}`
	got, err := Normalize([]byte(negative))
	if err != nil || len(got.Blocks) != 1 {
		t.Fatalf("Normalize() = %+v, %v", got.Blocks, err)
	}
}
