package audit

import (
	"context"
	"crypto/sha512"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/audit/auditdb"
	"github.com/morse/atlas/server/internal/platform/errs"
)

// Export formats.
const (
	FormatCSV   = "csv"
	FormatJSONL = "jsonl"
)

const (
	hoursPerDay  = 24
	decimalBase  = 10
	exportBatch  = 1000
	exportedName = "audit.exported"
	exportedWhat = "audit:log"
)

// Range is the days to export, both included: a report "from the 3rd to the 3rd" holds that whole day (UTC).
type Range struct {
	From time.Time
	To   time.Time
}

// ExportRequest asks for the events of a range in one format. Actor is who asked, for the audit trail.
type ExportRequest struct {
	Tenant uuid.UUID
	Actor  uuid.UUID
	Range  Range
	Format string
}

// Digest is the SHA-512 of what was written, and how many events it holds. The caller signs the Sum.
type Digest struct {
	Sum  []byte
	Rows int
}

// rowWriter writes events in one format.
type rowWriter interface {
	write(Event) error
	flush() error
}

var writers = map[string]func(io.Writer) rowWriter{
	FormatCSV:   func(w io.Writer) rowWriter { return newCSVWriter(w) },
	FormatJSONL: func(w io.Writer) rowWriter { return &jsonlWriter{enc: json.NewEncoder(w)} },
}

// Export writes the events of the range to w in the format asked, oldest first, and returns the SHA-512 of the
// bytes written. The export is a read, so the event that says it happened (audit.exported) is recorded after it.
func (s *Service) Export(ctx context.Context, req ExportRequest, w io.Writer) (Digest, error) {
	newWriter, ok := writers[req.Format]
	if !ok || req.Range.To.Before(req.Range.From) {
		return Digest{}, fmt.Errorf("audit: export needs a format (csv or jsonl) and a range that does not end before it starts: %w", errs.ErrInvalid)
	}
	hash := sha512.New()
	rows, err := s.writeRange(ctx, req, newWriter(io.MultiWriter(w, hash)))
	if err != nil {
		return Digest{}, err
	}
	digest := Digest{Sum: hash.Sum(nil), Rows: rows}
	return digest, s.Append(ctx, req.Tenant, exportedEntry(s.now(), req, digest))
}

func exportedEntry(at time.Time, req ExportRequest, d Digest) Entry {
	detail := fmt.Sprintf("from=%s to=%s format=%s rows=%d", req.Range.From.Format(time.DateOnly), req.Range.To.Format(time.DateOnly), req.Format, d.Rows)
	return Entry{At: at, Actor: req.Actor, Action: exportedName, Subject: exportedWhat, Detail: detail}
}

// writeRange pages through the events so a long log never sits in memory at once.
func (s *Service) writeRange(ctx context.Context, req ExportRequest, out rowWriter) (int, error) {
	rows := 0
	err := s.DB.InTenantTx(ctx, req.Tenant, func(tx pgx.Tx) error {
		q := auditdb.New(tx)
		for after := int64(0); ; {
			page, err := q.ListEventsBetween(ctx, betweenParams(req.Range, after))
			if err != nil {
				return fmt.Errorf("audit: export: %w", err)
			}
			for _, r := range page {
				if err := out.write(fromRow(r)); err != nil {
					return err
				}
				after, rows = r.ID, rows+1
			}
			if len(page) < exportBatch {
				return out.flush()
			}
		}
	})
	return rows, err
}

func betweenParams(r Range, after int64) auditdb.ListEventsBetweenParams {
	day := func(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC) }
	return auditdb.ListEventsBetweenParams{After: after, FromAt: day(r.From), UntilAt: day(r.To).Add(hoursPerDay * time.Hour), PageSize: exportBatch}
}

// csvHeader names the columns of the CSV export.
var csvHeader = []string{"id", "at", "actor_id", "action", "subject", "detail", "prev_hash", "hash"}

type csvWriter struct {
	out       *csv.Writer
	wroteHead bool
}

func newCSVWriter(w io.Writer) *csvWriter { return &csvWriter{out: csv.NewWriter(w)} }

func (c *csvWriter) write(e Event) error {
	if !c.wroteHead {
		c.wroteHead = true
		if err := c.out.Write(csvHeader); err != nil {
			return err
		}
	}
	actor := ""
	if e.ActorID != nil {
		actor = e.ActorID.String()
	}
	return c.out.Write([]string{strconv.FormatInt(e.ID, decimalBase), e.At.UTC().Format(time.RFC3339Nano), actor, e.Action, e.Subject, e.Detail, e.PrevHash, e.Hash})
}

// flush also writes the header of an export with no events, so an empty report is still a valid CSV.
func (c *csvWriter) flush() error {
	if !c.wroteHead {
		c.wroteHead = true
		_ = c.out.Write(csvHeader)
	}
	c.out.Flush()
	return c.out.Error()
}

type jsonlWriter struct{ enc *json.Encoder }

// exportedEvent is the JSON shape of an event, with the names of the CSV columns.
type exportedEvent struct {
	ID       int64      `json:"id"`
	At       time.Time  `json:"at"`
	ActorID  *uuid.UUID `json:"actor_id"`
	Action   string     `json:"action"`
	Subject  string     `json:"subject"`
	Detail   string     `json:"detail"`
	PrevHash string     `json:"prev_hash"`
	Hash     string     `json:"hash"`
}

func (j *jsonlWriter) write(e Event) error { return j.enc.Encode(exportedEvent(e)) }
func (j *jsonlWriter) flush() error        { return nil }
