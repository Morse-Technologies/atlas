//go:build integration

package audit_test

import (
	"bytes"
	"context"
	"crypto/sha512"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/platform/dbtest"
)

func record(t *testing.T, svc *audit.Service, tenant uuid.UUID, action string) {
	t.Helper()
	err := svc.DB.InTenantTx(context.Background(), tenant, func(tx pgx.Tx) error {
		return audit.Record(context.Background(), tx, audit.Entry{At: time.Now(), Action: action, Subject: "s"})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentWritersKeepOneLinearChain(t *testing.T) {
	d := dbtest.Start(t)
	svc, tenant := &audit.Service{DB: d}, dbtest.NewTenant(t, d)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() { defer wg.Done(); record(t, svc, tenant, "x") }()
	}
	wg.Wait()
	v, err := svc.Verify(context.Background(), tenant)
	if err != nil || !v.OK || v.Checked != 20 {
		t.Fatalf("Verify = %+v, %v; want 20 events in one valid chain", v, err)
	}
}

func TestTablesRejectEditsEvenFromTheApp(t *testing.T) {
	d := dbtest.Start(t)
	svc, tenant := &audit.Service{DB: d}, dbtest.NewTenant(t, d)
	record(t, svc, tenant, "x")
	err := d.InTenantTx(context.Background(), tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), "UPDATE audit.events SET detail = 'edited'")
		return err
	})
	if err == nil {
		t.Fatal("UPDATE on audit.events must fail")
	}
}

func TestTenantsHaveSeparateChains(t *testing.T) {
	d := dbtest.Start(t)
	svc := &audit.Service{DB: d}
	a, b := dbtest.NewTenant(t, d), dbtest.NewTenant(t, d)
	record(t, svc, a, "x")
	record(t, svc, b, "x")
	events, _, err := svc.List(context.Background(), a, audit.Page{Size: 50})
	if err != nil || len(events) != 1 || events[0].PrevHash != audit.GenesisHash {
		t.Fatalf("List = %+v, %v; tenant a must see only its own event, starting a fresh chain", events, err)
	}
}

func TestExportWritesTheDaysAskedForAndLeavesAnEvent(t *testing.T) {
	d := dbtest.Start(t)
	tenant, auditor := dbtest.NewTenant(t, d), uuid.New()
	svc := &audit.Service{DB: d}
	day := func(n int) time.Time { return time.Date(2026, 11, n, 23, 59, 59, 0, time.UTC) }
	for _, n := range []int{2, 3, 3, 4} {
		err := d.InTenantTx(context.Background(), tenant, func(tx pgx.Tx) error {
			return audit.Record(context.Background(), tx, audit.Entry{At: day(n), Action: "a", Subject: "s"})
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	req := audit.ExportRequest{Tenant: tenant, Actor: auditor, Format: audit.FormatJSONL, Range: audit.Range{From: day(3), To: day(3)}}
	digest, err := svc.Export(context.Background(), req, &out)
	if err != nil || digest.Rows != 2 || strings.Count(out.String(), "\n") != 2 {
		t.Fatalf("digest %+v, %v, output %q; the 3rd holds two events, edges included", digest, err, out.String())
	}
	sum := sha512.Sum512(out.Bytes())
	if !bytes.Equal(digest.Sum, sum[:]) {
		t.Fatal("the digest must be the SHA-512 of the bytes written")
	}
	events, _, err := svc.List(context.Background(), tenant, audit.Page{Size: 1})
	if err != nil || events[0].Action != "audit.exported" || events[0].ActorID == nil || *events[0].ActorID != auditor {
		t.Fatalf("events %+v, %v; want audit.exported by the auditor", events, err)
	}
}

func TestExportOfADayWithNoEventsIsAnEmptyCSVWithItsHeader(t *testing.T) {
	d := dbtest.Start(t)
	tenant := dbtest.NewTenant(t, d)
	var out bytes.Buffer
	day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	digest, err := (&audit.Service{DB: d}).Export(context.Background(), audit.ExportRequest{Tenant: tenant, Format: audit.FormatCSV, Range: audit.Range{From: day, To: day}}, &out)
	if err != nil || digest.Rows != 0 || !strings.HasPrefix(out.String(), "id,at") {
		t.Fatalf("digest %+v, %v, output %q", digest, err, out.String())
	}
}
