package documents

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/morse/atlas/server/internal/platform/errs"
)

// Block kinds (documents.blocks.kind).
const (
	KindHeading   = "heading"
	KindParagraph = "paragraph"
	KindTable     = "table"
	KindListItem  = "list_item"
	KindStep      = "step"
	KindWarning   = "warning"
)

// Rect is a block's box in PDF points with the origin at the top-left corner, as the viewer expects.
type Rect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// Block is one unit of the parsed document in reading order.
type Block struct {
	Kind string
	Page int
	Box  *Rect
	Text string
}

// Parsed is a normalized document.
type Parsed struct {
	Pages  int
	Blocks []Block
}

// The subset of DoclingDocument (docling-core JSON) that normalize reads.
type doclingDoc struct {
	Body   doclingNode            `json:"body"`
	Groups []doclingNode          `json:"groups"`
	Texts  []doclingText          `json:"texts"`
	Tables []doclingTable         `json:"tables"`
	Pages  map[string]doclingPage `json:"pages"`
}

type doclingRef struct {
	Ref string `json:"$ref"`
}

type doclingNode struct {
	Children []doclingRef `json:"children"`
}

type doclingText struct {
	Label      string        `json:"label"`
	Text       string        `json:"text"`
	Enumerated bool          `json:"enumerated"`
	Prov       []doclingProv `json:"prov"`
	Children   []doclingRef  `json:"children"`
}

type doclingTable struct {
	Prov []doclingProv `json:"prov"`
	Data struct {
		Grid [][]struct {
			Text string `json:"text"`
		} `json:"grid"`
	} `json:"data"`
}

type doclingProv struct {
	PageNo int         `json:"page_no"`
	BBox   doclingBBox `json:"bbox"`
}

type doclingBBox struct {
	L           float64 `json:"l"`
	T           float64 `json:"t"`
	R           float64 `json:"r"`
	B           float64 `json:"b"`
	CoordOrigin string  `json:"coord_origin"`
}

type doclingPage struct {
	Size struct {
		Height float64 `json:"height"`
	} `json:"size"`
}

const bottomLeftOrigin = "BOTTOMLEFT"

// kindByLabel maps Docling text labels to block kinds. Page headers and footers are absent on purpose:
// they repeat on every page and only add noise. Unknown labels become paragraphs so no text is lost.
var kindByLabel = map[string]string{
	"title":          KindHeading,
	"section_header": KindHeading,
	"list_item":      KindListItem,
	"text":           KindParagraph,
	"paragraph":      KindParagraph,
	"caption":        KindParagraph,
	"footnote":       KindParagraph,
	"code":           KindParagraph,
	"formula":        KindParagraph,
}

var skippedLabels = map[string]bool{"page_header": true, "page_footer": true}

// warningPrefix recognizes safety warnings in the languages Atlas ships with.
var warningPrefix = regexp.MustCompile(`(?i)^\s*(warning|caution|danger|advertencia|precauci[oó]n|atenci[oó]n|peligro)\b`)

// Normalize turns DoclingDocument JSON into blocks in reading order.
func Normalize(raw []byte) (Parsed, error) {
	var doc doclingDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Parsed{}, errs.NewPermanent(errs.ReasonCorrupt, fmt.Errorf("parsed document is not valid JSON: %w", err))
	}
	w := walker{doc: doc, visited: map[string]bool{}}
	w.visitAll(doc.Body.Children)
	return Parsed{Pages: len(doc.Pages), Blocks: w.blocks}, nil
}

// walker follows the body tree. Refs look like "#/texts/3", "#/groups/1" or "#/tables/0".
// Each ref is visited once, so a malformed tree with a cycle cannot recurse forever.
type walker struct {
	doc     doclingDoc
	blocks  []Block
	visited map[string]bool
}

func (w *walker) visitAll(refs []doclingRef) {
	for _, r := range refs {
		w.visit(r.Ref)
	}
}

func (w *walker) visit(ref string) {
	collection, index, ok := splitRef(ref)
	if !ok || w.visited[ref] {
		return
	}
	w.visited[ref] = true
	switch {
	case collection == "texts" && index < len(w.doc.Texts):
		w.addText(w.doc.Texts[index])
	case collection == "tables" && index < len(w.doc.Tables):
		w.addTable(w.doc.Tables[index])
	case collection == "groups" && index < len(w.doc.Groups):
		w.visitAll(w.doc.Groups[index].Children)
	}
}

func splitRef(ref string) (string, int, bool) {
	parts := strings.Split(strings.TrimPrefix(ref, "#/"), "/")
	const refParts = 2 // collection and index
	if len(parts) != refParts {
		return "", 0, false
	}
	index, err := strconv.Atoi(parts[1])
	return parts[0], index, err == nil && index >= 0
}

func (w *walker) addText(t doclingText) {
	text := strings.TrimSpace(t.Text)
	if text != "" && !skippedLabels[t.Label] {
		page, box := w.location(t.Prov)
		w.blocks = append(w.blocks, Block{Kind: textKind(t, text), Page: page, Box: box, Text: text})
	}
	w.visitAll(t.Children)
}

func textKind(t doclingText, text string) string {
	kind, ok := kindByLabel[t.Label]
	switch {
	case !ok:
		kind = KindParagraph
	case kind == KindListItem && t.Enumerated:
		return KindStep
	}
	if kind == KindParagraph && warningPrefix.MatchString(text) {
		return KindWarning
	}
	return kind
}

// addTable renders the grid as one row per line with cells separated by " | ".
func (w *walker) addTable(t doclingTable) {
	rows := make([]string, 0, len(t.Data.Grid))
	for _, row := range t.Data.Grid {
		cells := make([]string, len(row))
		for i, c := range row {
			cells[i] = strings.TrimSpace(c.Text)
		}
		rows = append(rows, strings.Join(cells, " | "))
	}
	page, box := w.location(t.Prov)
	w.blocks = append(w.blocks, Block{Kind: KindTable, Page: page, Box: box, Text: strings.Join(rows, "\n")})
}

// noPage marks blocks without a page (spreadsheets); real pages start at 1.
const noPage = 0

// location returns the first provenance's page and its box with a top-left origin.
// Items without provenance get noPage and no box.
func (w *walker) location(prov []doclingProv) (int, *Rect) {
	if len(prov) == 0 {
		return noPage, nil
	}
	p := prov[0]
	pageHeight := w.doc.Pages[strconv.Itoa(p.PageNo)].Size.Height
	return p.PageNo, topLeftRect(p.BBox, pageHeight)
}

func topLeftRect(b doclingBBox, pageHeight float64) *Rect {
	if b.CoordOrigin == bottomLeftOrigin {
		return &Rect{X: b.L, Y: pageHeight - b.T, W: b.R - b.L, H: b.T - b.B}
	}
	return &Rect{X: b.L, Y: b.T, W: b.R - b.L, H: b.B - b.T}
}
