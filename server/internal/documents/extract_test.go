package documents

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func heading(text string) Block   { return Block{Kind: KindHeading, Page: 1, Text: text} }
func paragraph(text string) Block { return Block{Kind: KindParagraph, Page: 1, Text: text} }

func onPage(page int, b Block) Block {
	b.Page = page
	return b
}

var titleCases = map[string]struct {
	blocks []Block
	want   string
}{
	"the first heading":                    {[]Block{heading("Montaje del husillo"), heading("Seguridad")}, "Montaje del husillo"},
	"a numbered section is not a title":    {[]Block{heading("1 Objeto"), heading("2.1 Alcance")}, ""},
	"a title after a paragraph":            {[]Block{paragraph("Calidad / Línea 2"), heading("Montaje del husillo")}, "Montaje del husillo"},
	"the first heading that is no section": {[]Block{heading("4.2 Tuerca"), heading("Anexo técnico")}, "Anexo técnico"},
	"no heading":                           {[]Block{paragraph("Solo texto")}, ""},
	"an empty heading is skipped":          {[]Block{heading("  "), heading("Título")}, "Título"},
}

func TestTheTitleIsTheFirstHeadingThatIsNotANumberedSection(t *testing.T) {
	for name, tt := range titleCases {
		t.Run(name, func(t *testing.T) {
			if got := titleOf(tt.blocks); got != tt.want {
				t.Fatalf("titleOf() = %q, want %q", got, tt.want)
			}
		})
	}
}

var codeCases = map[string]struct {
	blocks   []Block
	file     string
	wantCode reading
	wantRev  reading
}{
	"labelled code and revision in the title block": {[]Block{paragraph("Código: PNT-014   Rev. 3")}, "x.pdf",
		reading{"PNT-014", "titleblock", confidenceLabelledCode}, reading{value: "3", source: "titleblock"}},
	"revision written out":                        {[]Block{paragraph("Revisión: 2")}, "x.pdf", reading{}, reading{value: "2", source: "titleblock"}},
	"edition":                                     {[]Block{paragraph("Edición 4")}, "x.pdf", reading{}, reading{value: "4", source: "titleblock"}},
	"code and revision in name":                   {nil, "Calidad/PNT-014-rev3.pdf", reading{"PNT-014", "path", confidenceNameCode}, reading{value: "3", source: "path"}},
	"the title block wins":                        {[]Block{paragraph("Ref. PNT-020 Rev 5")}, "PNT-014-rev3.pdf", reading{"PNT-020", "titleblock", confidenceLabelledCode}, reading{value: "5", source: "titleblock"}},
	"a code in the body is another document":      {[]Block{onPage(3, paragraph("Aplicar PNT-021 (bloqueo). Código: PNT-099 Rev 9"))}, "notas.pdf", reading{}, reading{}},
	"an unlabelled code on page one is not taken": {[]Block{paragraph("Calidad / Zona 2, EQ-07, EQ-08")}, "notas.pdf", reading{}, reading{}},
	"a standard in the name is not a code":        {nil, "ISO-9001-manual.pdf", reading{}, reading{}},
	"nothing to read":                             {nil, "notas.pdf", reading{}, reading{}},
}

func TestCodeAndRevisionComeFromTheTitleBlockThenFromTheFileName(t *testing.T) {
	for name, tt := range codeCases {
		t.Run(name, func(t *testing.T) {
			code, revision := codeAndRevision(tt.blocks, tt.file)
			if code != tt.wantCode || revision != tt.wantRev {
				t.Fatalf("codeAndRevision() = %+v, %+v; want %+v, %+v", code, revision, tt.wantCode, tt.wantRev)
			}
		})
	}
}

var languageCases = map[string]struct{ text, want string }{
	"spanish":       {"Este procedimiento describe el montaje del husillo en los centros de mecanizado de la línea y que se aplica al taller", "es"},
	"english":       {"This procedure is for the assembly of the spindle on the machining centres and it is to be used with the tool", "en"},
	"too few words": {"Montaje EQ-07 40 Nm TW-03", ""},
	"mixed":         {"el montaje de la tuerca and the torque of the spindle in the line y que se aplica to the taller for", ""},
}

func TestTheLanguageIsTheOneWhoseCommonWordsAreMoreFrequent(t *testing.T) {
	for name, tt := range languageCases {
		t.Run(name, func(t *testing.T) {
			if got := languageOf(tt.text); got != tt.want {
				t.Fatalf("languageOf() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The golden PDF has no title block: its name carries the code and the revision, its headings are numbered sections.
func TestWhatTheRulesReadInTheGoldenPDF(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/golden/PNT-014-rev3.pdf.blocks.json")
	if err != nil {
		t.Fatal(err)
	}
	var parsed Parsed
	if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed.Blocks) == 0 {
		t.Fatalf("golden blocks: %d, %v", len(parsed.Blocks), err)
	}
	got := extract(parsed, "Calidad/PNT-014-rev3.pdf")
	if got.title.value != "" || got.code.value != "PNT-014" || got.code.source != "path" || got.revision.value != "3" || got.language.value != "es" {
		t.Fatalf("extract() = %+v", got)
	}
}

func TestEveryRuleStatesWhereItReadAndHowMuchToTrustIt(t *testing.T) {
	got := extract(Parsed{Blocks: []Block{heading("Montaje"), paragraph(strings.Repeat("de la y el ", 4))}}, "PNT-014.pdf")
	if got.title.source != "titleblock" || got.title.confidence != confidenceTitle || got.language.confidence != confidenceLanguage || got.code.source != "path" {
		t.Fatalf("extract() = %+v", got)
	}
}
