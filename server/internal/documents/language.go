package documents

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/documents/documentsdb"
	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/jobs"
	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/platform/errs"
)

// Languages of the word search. "none" ignores accents but keeps every word as written (no stems); it is what an
// organization has until it chooses.
const (
	LanguageSpanish = "es"
	LanguageEnglish = "en"
	LanguageNone    = "none"
)

// relangBatch is how many passages one job run re-labels, so a large corpus never holds one long transaction.
const relangBatch = 5000

var searchLanguages = map[string]bool{LanguageSpanish: true, LanguageEnglish: true, LanguageNone: true}

// SearchLanguage returns the language the organization chose for word search.
func (l *Library) SearchLanguage(ctx context.Context, who Actor) (string, error) {
	language := LanguageNone
	err := l.DB.InScopeTx(ctx, db.Scope{Tenant: who.TenantID, User: who.UserID}, func(tx pgx.Tx) error {
		stored, getErr := documentsdb.New(tx).GetSearchLanguage(ctx)
		switch {
		case errors.Is(getErr, pgx.ErrNoRows):
			return nil // never chosen
		case getErr != nil:
			return fmt.Errorf("read search language: %w", getErr)
		}
		language = stored
		return nil
	})
	return language, err
}

// SetSearchLanguage changes the language of the word search. The passages already indexed are re-labelled by a job;
// until it finishes, searches still find them by words as they were indexed.
func (l *Library) SetSearchLanguage(ctx context.Context, who Actor, language string) error {
	if !searchLanguages[language] {
		return fmt.Errorf("unknown search language %q: %w", language, errs.ErrInvalid)
	}
	return l.DB.InScopeTx(ctx, db.Scope{Tenant: who.TenantID, User: who.UserID}, func(tx pgx.Tx) error {
		if err := documentsdb.New(tx).SetSearchLanguage(ctx, language); err != nil {
			return fmt.Errorf("set search language: %w", err)
		}
		entry := audit.Entry{At: l.Now(), Actor: who.UserID, Action: "settings.updated", Subject: "settings", Detail: "fields=search_language"}
		if err := audit.Record(ctx, tx, entry); err != nil {
			return err
		}
		return jobs.Enqueue(ctx, tx, events.SearchLanguageChanged{TenantID: who.TenantID})
	})
}

// OnSearchLanguageChanged re-labels one batch of passages with the organization's language and, if the batch was
// full, queues the next one in the same transaction.
func (m *Module) OnSearchLanguageChanged(ctx context.Context, args events.SearchLanguageChanged) error {
	return m.DB.InTenantTx(ctx, args.TenantID, func(tx pgx.Tx) error {
		changed, err := documentsdb.New(tx).RelabelChunks(ctx, relangBatch)
		if err != nil {
			return fmt.Errorf("relabel passages: %w", err)
		}
		if changed < relangBatch {
			return nil
		}
		return jobs.Enqueue(ctx, tx, args)
	})
}
