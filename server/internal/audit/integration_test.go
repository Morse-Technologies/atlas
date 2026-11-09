//go:build integration

package audit_test

import (
	"context"
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
