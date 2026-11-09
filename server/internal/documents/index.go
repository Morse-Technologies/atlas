package documents

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/documents/documentsdb"
	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/jobs"
	"github.com/morse/atlas/server/internal/platform/models/tei"
	"github.com/morse/atlas/server/internal/sources"
)

// Pipeline steps after enrich.
const (
	StepChunk = "chunk"
	StepEmbed = "embed"
	StepIndex = "index"
	// charsPerToken estimates tokens when no tokenizer is configured (installs that search by words only).
	charsPerToken = 4
)

// Embedder turns passages into vectors. *tei.Client and *ollama.Client implement it.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// TokenCounter counts the tokens of the embedding model. *tei.Client implements it.
type TokenCounter interface {
	CountTokens(ctx context.Context, texts []string) ([]int, error)
}

// Chunk cuts the version's blocks into passages and queues their embedding.
func (m *Module) Chunk(ctx context.Context, args events.VersionChunk) error {
	v, err := m.loadVersion(ctx, args.TenantID, args.VersionID)
	if err != nil {
		return err
	}
	title, blocks, err := m.readBlocks(ctx, v)
	if err != nil {
		return err
	}
	counts, err := m.countTokens(ctx, blocks)
	if err != nil {
		return err
	}
	set := chunkSet{v: v, chunks: BuildChunks(title, blocks, counts)}
	return m.DB.InTenantTx(ctx, args.TenantID, func(tx pgx.Tx) error { return storeChunks(ctx, tx, set) })
}

func storeChunks(ctx context.Context, tx pgx.Tx, set chunkSet) error {
	if err := replaceChunks(ctx, tx, set); err != nil {
		return err
	}
	if err := sources.RecordStep(ctx, tx, set.v.step(StepChunk, sources.StepDone)); err != nil {
		return err
	}
	return jobs.Enqueue(ctx, tx, events.VersionEmbed{TenantID: set.v.tenantID, VersionID: set.v.row.ID})
}

func (m *Module) readBlocks(ctx context.Context, v version) (string, []Block, error) {
	var title string
	var blocks []Block
	err := m.DB.InTenantTx(ctx, v.tenantID, func(tx pgx.Tx) error {
		q := documentsdb.New(tx)
		info, err := q.GetVersionTitle(ctx, v.row.ID)
		if err != nil {
			return fmt.Errorf("get title: %w", err)
		}
		rows, err := q.ListBlocks(ctx, v.row.ID)
		if err != nil {
			return fmt.Errorf("list blocks: %w", err)
		}
		title = info.Title
		blocks, err = toBlocks(rows)
		return err
	})
	return title, blocks, err
}

func toBlocks(rows []documentsdb.ListBlocksRow) ([]Block, error) {
	blocks := make([]Block, len(rows))
	for i, r := range rows {
		blocks[i] = Block{Kind: r.Kind, Page: int(r.Page), Text: r.Text}
		if len(r.Bbox) > 0 {
			var box Rect
			if err := json.Unmarshal(r.Bbox, &box); err != nil {
				return nil, fmt.Errorf("block %d box: %w", r.Ord, err)
			}
			blocks[i].Box = &box
		}
	}
	return blocks, nil
}

func (m *Module) countTokens(ctx context.Context, blocks []Block) ([]int, error) {
	texts := make([]string, len(blocks))
	for i, b := range blocks {
		texts[i] = b.Text
	}
	if m.Tokens == nil {
		return approximateTokens(texts), nil
	}
	return m.Tokens.CountTokens(ctx, texts)
}

func approximateTokens(texts []string) []int {
	counts := make([]int, len(texts))
	for i, t := range texts {
		counts[i] = len(t)/charsPerToken + 1
	}
	return counts
}

// chunkSet is the chunks of one version, to store.
type chunkSet struct {
	v      version
	chunks []Chunk
}

// replaceChunks swaps the version's chunks for new ones. Ids are derived from (version, ord), so a rerun
// produces the same ids.
func replaceChunks(ctx context.Context, tx pgx.Tx, set chunkSet) error {
	q := documentsdb.New(tx)
	if err := q.DeleteChunks(ctx, set.v.row.ID); err != nil {
		return fmt.Errorf("delete chunks: %w", err)
	}
	params, err := chunkParams(set)
	if err != nil {
		return err
	}
	if err := q.InsertChunks(ctx, params); err != nil {
		return fmt.Errorf("insert chunks: %w", err)
	}
	return nil
}

func chunkParams(set chunkSet) (documentsdb.InsertChunksParams, error) {
	p := documentsdb.InsertChunksParams{VersionID: set.v.row.ID, DocumentID: set.v.row.DocumentID}
	for _, c := range set.chunks {
		box, err := boxJSON(c.Box)
		if err != nil {
			return p, err
		}
		p.Ids = append(p.Ids, chunkID(set.v.row.ID, c.Ord))
		p.Ords = append(p.Ords, int32(c.Ord))
		p.Pages = append(p.Pages, int32(c.Page))
		p.Sections = append(p.Sections, c.Section)
		p.Headers = append(p.Headers, c.Header)
		p.Texts = append(p.Texts, c.Text)
		p.Codes = append(p.Codes, c.Codes)
		p.TokenCounts = append(p.TokenCounts, int32(c.Tokens))
		p.Bboxes = append(p.Bboxes, box)
		p.Suspects = append(p.Suspects, c.Suspect)
	}
	return p, nil
}

func chunkID(versionID uuid.UUID, ord int) uuid.UUID {
	return uuid.NewSHA1(versionID, []byte(strconv.Itoa(ord)))
}

// Embed computes the vectors of the passages that do not have one for the active model, then queues indexing.
// With no embedder configured the version is indexed for word search only.
func (m *Module) Embed(ctx context.Context, args events.VersionEmbed) error {
	v, err := m.loadVersion(ctx, args.TenantID, args.VersionID)
	if err != nil {
		return err
	}
	vectors, err := m.vectorsFor(ctx, v)
	if err != nil {
		return err
	}
	return m.DB.InTenantTx(ctx, args.TenantID, func(tx pgx.Tx) error {
		if err := m.storeVectors(ctx, tx, vectors); err != nil {
			return err
		}
		if err := sources.RecordStep(ctx, tx, v.step(StepEmbed, embedStatus(m.Embedder))); err != nil {
			return err
		}
		return jobs.Enqueue(ctx, tx, events.VersionIndex{TenantID: args.TenantID, VersionID: args.VersionID})
	})
}

func embedStatus(e Embedder) string {
	if e == nil {
		return sources.StepSkipped
	}
	return sources.StepDone
}

// embedded is the passages of a version with their vectors.
type embedded struct {
	versionID uuid.UUID
	ids       []uuid.UUID
	vectors   [][]float32
}

func (m *Module) vectorsFor(ctx context.Context, v version) (embedded, error) {
	out := embedded{versionID: v.row.ID}
	if m.Embedder == nil {
		return out, nil
	}
	var texts []string
	err := m.DB.InTenantTx(ctx, v.tenantID, func(tx pgx.Tx) error {
		rows, err := documentsdb.New(tx).ListChunksToEmbed(ctx, documentsdb.ListChunksToEmbedParams{VersionID: v.row.ID, EmbedModel: m.EmbedModel})
		for _, r := range rows {
			out.ids = append(out.ids, r.ID)
			texts = append(texts, r.ContextHeader+"\n"+r.Text)
		}
		return err
	})
	if err != nil || len(texts) == 0 {
		return out, err
	}
	out.vectors, err = m.Embedder.Embed(ctx, texts)
	return out, err
}

func (m *Module) storeVectors(ctx context.Context, tx pgx.Tx, e embedded) error {
	if len(e.ids) == 0 {
		return nil
	}
	literals := make([]string, len(e.vectors))
	for i, vec := range e.vectors {
		if len(vec) != tei.Dimension {
			return fmt.Errorf("embedding has %d dimensions, the index expects %d", len(vec), tei.Dimension)
		}
		literals[i] = tei.VectorLiteral(vec)
	}
	err := documentsdb.New(tx).InsertEmbeddings(ctx, documentsdb.InsertEmbeddingsParams{ChunkIds: e.ids, EmbedModel: m.EmbedModel, Vectors: literals})
	if err != nil {
		return fmt.Errorf("insert embeddings: %w", err)
	}
	return nil
}

// Index makes the version's passages the ones search finds, and retires the previous version's, in one
// transaction. If a newer version became current meanwhile, this one is skipped.
func (m *Module) Index(ctx context.Context, args events.VersionIndex) error {
	v, err := m.loadVersion(ctx, args.TenantID, args.VersionID)
	if err != nil {
		return err
	}
	return m.DB.InTenantTx(ctx, args.TenantID, func(tx pgx.Tx) error {
		status, err := publish(ctx, tx, v)
		if err != nil {
			return err
		}
		if err := sources.RecordStep(ctx, tx, v.step(StepIndex, status)); err != nil {
			return err
		}
		if status != sources.StepDone {
			return nil
		}
		return jobs.Enqueue(ctx, tx, events.VersionIndexed{TenantID: args.TenantID, VersionID: v.row.ID, DocumentID: v.row.DocumentID})
	})
}

func publish(ctx context.Context, tx pgx.Tx, v version) (string, error) {
	q := documentsdb.New(tx)
	current, err := q.IsCurrentVersion(ctx, documentsdb.IsCurrentVersionParams{DocumentID: v.row.DocumentID, VersionID: v.row.ID})
	if err != nil || !current {
		return sources.StepSkipped, err
	}
	acl, err := documentACLSet(ctx, tx, v.row.DocumentID)
	if err != nil {
		return "", err
	}
	if err := retireOthers(ctx, q, v); err != nil {
		return "", err
	}
	if err := q.PublishChunks(ctx, documentsdb.PublishChunksParams{VersionID: v.row.ID, AclSetID: &acl}); err != nil {
		return "", fmt.Errorf("publish chunks: %w", err)
	}
	if err := q.PublishEmbeddings(ctx, documentsdb.PublishEmbeddingsParams{VersionID: v.row.ID, AclSetID: &acl}); err != nil {
		return "", fmt.Errorf("publish embeddings: %w", err)
	}
	return sources.StepDone, q.MarkVersionIndexed(ctx, v.row.ID)
}

// retireOthers stops search from finding the document's older versions.
func retireOthers(ctx context.Context, q *documentsdb.Queries, v version) error {
	other := documentsdb.RetireOtherChunksParams{DocumentID: v.row.DocumentID, VersionID: v.row.ID}
	if err := q.RetireOtherChunks(ctx, other); err != nil {
		return fmt.Errorf("retire chunks: %w", err)
	}
	if err := q.RetireOtherEmbeddings(ctx, documentsdb.RetireOtherEmbeddingsParams(other)); err != nil {
		return fmt.Errorf("retire embeddings: %w", err)
	}
	return nil
}

// documentACLSet is the set the passages of a document belong to: its source's, or the default one.
func documentACLSet(ctx context.Context, tx pgx.Tx, document uuid.UUID) (uuid.UUID, error) {
	set, err := documentsdb.New(tx).GetDocumentACLSet(ctx, document)
	if err != nil {
		return uuid.Nil, fmt.Errorf("get document acl set: %w", err)
	}
	if set != nil {
		return *set, nil
	}
	return access.DefaultACLSet(ctx, tx)
}

// ApplyAccess gives every document of a source, and its passages and vectors, the source's new set (nil: the
// default one). It runs in the transaction that changes the source's readers.
func ApplyAccess(ctx context.Context, tx pgx.Tx, c sources.AccessChange) error {
	effective := uuid.Nil
	if c.SetID != nil {
		effective = *c.SetID
	} else {
		var err error
		if effective, err = access.DefaultACLSet(ctx, tx); err != nil {
			return err
		}
	}
	q := documentsdb.New(tx)
	if err := q.SetSourceDocumentsACL(ctx, documentsdb.SetSourceDocumentsACLParams{SourceID: c.SourceID, AclSetID: c.SetID}); err != nil {
		return fmt.Errorf("set documents acl: %w", err)
	}
	if err := q.SetSourceChunksACL(ctx, documentsdb.SetSourceChunksACLParams{SourceID: c.SourceID, AclSetID: &effective}); err != nil {
		return fmt.Errorf("set chunks acl: %w", err)
	}
	if err := q.SetSourceEmbeddingsACL(ctx, documentsdb.SetSourceEmbeddingsACLParams{SourceID: c.SourceID, AclSetID: &effective}); err != nil {
		return fmt.Errorf("set embeddings acl: %w", err)
	}
	return nil
}
