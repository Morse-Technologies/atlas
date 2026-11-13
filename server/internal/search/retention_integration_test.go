//go:build integration

package search

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/audit"
)

// answerAskedAt stores a conversation with one answered question asked at the given time.
func (w world) answerAskedAt(at time.Time) {
	w.t.Helper()
	err := w.svc.DB.InTenantTx(w.ctx, w.who.TenantID, func(tx pgx.Tx) error {
		var conversation uuid.UUID
		err := tx.QueryRow(w.ctx, `INSERT INTO search.conversations (user_id, title, updated_at) VALUES ($1, 'q', $2) RETURNING id`,
			w.who.UserID, at).Scan(&conversation)
		if err != nil {
			return err
		}
		_, err = tx.Exec(w.ctx, `INSERT INTO search.answers (conversation_id, user_id, question, asked_at, outcome)
			VALUES ($1, $2, 'q', $3, 'answered')`, conversation, w.who.UserID, at)
		return err
	})
	if err != nil {
		w.t.Fatal(err)
	}
}

func (w world) count(table string) (n int) {
	w.t.Helper()
	err := w.svc.DB.InTenantTx(w.ctx, w.who.TenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(w.ctx, "SELECT count(*) FROM "+table).Scan(&n)
	})
	if err != nil {
		w.t.Fatal(err)
	}
	return n
}

func TestOldAnswersAreErasedAndTheChainStaysIntact(t *testing.T) {
	w := newWorld(t, Config{})
	now := time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC)
	retentionDays := access.DefaultSettings().RetentionDays
	w.answerAskedAt(now.AddDate(0, 0, -retentionDays-1))
	w.answerAskedAt(now.AddDate(0, 0, -1))
	log := &audit.Service{DB: w.svc.DB}
	if err := log.Append(w.ctx, w.who.TenantID, audit.Entry{At: now.AddDate(0, 0, -retentionDays-1), Action: "answer.created"}); err != nil {
		t.Fatal(err)
	}

	if err := w.svc.PurgeExpired(w.ctx, w.who.TenantID, now); err != nil {
		t.Fatal(err)
	}

	if answers, conversations := w.count("search.answers"), w.count("search.conversations"); answers != 1 || conversations != 1 {
		t.Fatalf("%d answers and %d conversations left, want the recent one only", answers, conversations)
	}
	if v, err := log.Verify(w.ctx, w.who.TenantID); err != nil || !v.OK || v.Checked != 1 {
		t.Fatalf("Verify = %+v, %v; want the audit chain whole", v, err)
	}
}

func TestAConversationJustOpenedIsNotErased(t *testing.T) {
	w := newWorld(t, Config{})
	now := time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC)
	err := w.svc.DB.InTenantTx(w.ctx, w.who.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(w.ctx, `INSERT INTO search.conversations (user_id, title, updated_at) VALUES ($1, 'new', $2)`, w.who.UserID, now)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := w.svc.PurgeExpired(w.ctx, w.who.TenantID, now); err != nil {
		t.Fatal(err)
	}
	if w.count("search.conversations") != 1 {
		t.Fatal("a conversation with no answer yet must survive")
	}
}
