package documents

import (
	"regexp"
	"strings"
)

// Chunk sizes in tokens of the embedder (ATLAS.md §8).
const (
	targetChunkTokens  = 512
	overlapChunkTokens = 80
	sectionSeparator   = " > "
	sentenceEnds       = ".?!\n"
)

// Chunk is a passage the search finds and the answer cites. Its text never starts or ends in the middle of a
// step, a table or a warning.
type Chunk struct {
	Ord     int
	Page    int
	Section string
	Header  string
	Text    string
	Tokens  int
	Box     *Rect
	Codes   string
	Suspect bool
}

// sized is a block with its token count.
type sized struct {
	Block
	tokens int
}

// atomicKinds are never split, and never repeated as overlap.
var atomicKinds = map[string]bool{KindTable: true, KindStep: true, KindWarning: true}

// codePattern finds document codes and standards (PNT-014, ISO 9001:2015) so they match exactly in the search.
var codePattern = regexp.MustCompile(`\b[A-Z]{2,6}-\d{1,5}\b|\bISO[ -]?\d{3,6}(?::\d{4})?\b`)

// injectionPattern flags text that talks to an AI instead of to the reader (ATLAS.md §11.5). A flag marks the
// passage so answers that cite it carry a warning; it does not hide it.
var injectionPattern = regexp.MustCompile(`(?i)ignor[ae]\s+(todas?\s+)?(las\s+)?instrucciones|ignore\s+(all\s+)?(the\s+)?(previous|above|prior)\s+instructions|system\s+prompt|you\s+are\s+now\b|<\|im_(start|end)\|>|\bnueva\s+instrucci[oó]n\b`)

// BuildChunks groups blocks into chunks. tokens[i] is the token count of blocks[i].
func BuildChunks(title string, blocks []Block, tokens []int) []Chunk {
	b := &builder{title: title}
	for i, block := range blocks {
		if strings.TrimSpace(block.Text) == "" {
			continue
		}
		b.add(sized{Block: block, tokens: tokens[i]})
	}
	b.flush(keepNothing)
	return b.out
}

type builder struct {
	title   string
	section string
	cur     []sized
	total   int
	out     []Chunk
}

// carry says what a flush keeps for the next chunk.
type carry int

const (
	keepNothing carry = iota
	keepOverlap
)

func (b *builder) add(s sized) {
	switch {
	case s.Kind == KindHeading:
		b.flush(keepNothing) // no overlap across sections
		b.section = strings.TrimSpace(s.Text)
		b.push(s)
	case !atomicKinds[s.Kind] && s.tokens > targetChunkTokens:
		for _, piece := range splitLong(s) {
			b.place(piece) // pieces are placed as they are: cutting them again could never end
		}
	default:
		b.place(s)
	}
}

// place adds a block to the current chunk, closing it first if the block would not fit.
func (b *builder) place(s sized) {
	if b.total+s.tokens > targetChunkTokens && len(b.cur) > 0 {
		b.flush(keepOverlap)
	}
	b.push(s)
}

func (b *builder) push(s sized) {
	b.cur = append(b.cur, s)
	b.total += s.tokens
}

// flush closes the current chunk. With keepOverlap, the last plain blocks (up to the overlap size) open the next one.
func (b *builder) flush(keep carry) {
	if len(b.cur) == 0 {
		return
	}
	b.out = append(b.out, b.makeChunk())
	var kept []sized
	if keep == keepOverlap {
		kept = overlapTail(b.cur)
	}
	b.cur, b.total = nil, 0
	for _, s := range kept {
		b.push(s)
	}
}

// overlapTail returns the trailing non-atomic blocks that fit in the overlap, in order.
func overlapTail(blocks []sized) []sized {
	var tail []sized
	sum := 0
	for i := len(blocks) - 1; i >= 0; i-- {
		s := blocks[i]
		if atomicKinds[s.Kind] || s.Kind == KindHeading || sum+s.tokens > overlapChunkTokens {
			break
		}
		tail = append([]sized{s}, tail...)
		sum += s.tokens
	}
	return tail
}

func (b *builder) makeChunk() Chunk {
	texts := make([]string, len(b.cur))
	for i, s := range b.cur {
		texts[i] = strings.TrimSpace(s.Text)
	}
	text := strings.Join(texts, "\n")
	first := b.cur[0]
	return Chunk{Ord: len(b.out), Page: first.Page, Section: b.section, Header: b.header(), Text: text,
		Tokens: b.total, Box: firstBox(b.cur), Codes: strings.Join(codePattern.FindAllString(text, -1), " "),
		Suspect: injectionPattern.MatchString(text)}
}

func (b *builder) header() string {
	if b.section == "" {
		return b.title
	}
	return b.title + sectionSeparator + b.section
}

func firstBox(blocks []sized) *Rect {
	for _, s := range blocks {
		if s.Box != nil {
			return s.Box
		}
	}
	return nil
}

// splitLong cuts a paragraph that alone exceeds the target at sentence ends. Token counts are shared out by length.
func splitLong(s sized) []sized {
	var pieces []sized
	var cur strings.Builder
	flush := func() {
		if text := strings.TrimSpace(cur.String()); text != "" {
			pieces = append(pieces, sized{Block: Block{Kind: s.Kind, Page: s.Page, Box: s.Box, Text: text}, tokens: s.tokens * len(text) / max(len(s.Text), 1)})
		}
		cur.Reset()
	}
	perPiece := len(s.Text) * targetChunkTokens / s.tokens
	for _, r := range s.Text {
		cur.WriteRune(r)
		if cur.Len() >= perPiece && strings.ContainsRune(sentenceEnds, r) {
			flush()
		}
	}
	flush()
	return pieces
}
