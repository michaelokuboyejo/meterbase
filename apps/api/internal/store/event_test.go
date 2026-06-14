package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// ── DEDUP (mandatory) ─────────────────────────────────────────────────────────

// Posting the same event id twice must yield received=1, duplicates=1,
// and exactly one row in the events table.
func TestEventStore_Dedup(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	es := NewTimescaleEventStore(pool)
	ctx := context.Background()

	orgID := createTestOrg(t, os, t.Name())
	eventTime := time.Now().UTC().Truncate(time.Microsecond)
	evt := IngestEvent{ID: "dedup-evt-1", Type: "api_req", Subject: "user_1", Time: &eventTime, Data: json.RawMessage(`{}`)}

	rec1, dup1, err := es.IngestEvents(ctx, orgID, []IngestEvent{evt})
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	if rec1 != 1 || dup1 != 0 {
		t.Errorf("first ingest: want received=1 dup=0, got received=%d dup=%d", rec1, dup1)
	}

	// Second call — same id, same time.
	rec2, dup2, err := es.IngestEvents(ctx, orgID, []IngestEvent{evt})
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if rec2 != 0 || dup2 != 1 {
		t.Errorf("second ingest: want received=0 dup=1, got received=%d dup=%d", rec2, dup2)
	}

	// Exactly one row in the table.
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM events WHERE org_id=$1 AND id=$2`, orgID, "dedup-evt-1").Scan(&count); err != nil {
		t.Fatalf("count scan: %v", err)
	}
	if count != 1 {
		t.Errorf("row count: want 1, got %d", count)
	}
}

// ── BATCH (mandatory) ─────────────────────────────────────────────────────────

// A batch containing a duplicate must report the correct received/duplicate split.
// Batch: [evt-a, evt-b, evt-a (dup)] → received=2, duplicates=1.
func TestEventStore_BatchMixed(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	es := NewTimescaleEventStore(pool)
	ctx := context.Background()

	orgID := createTestOrg(t, os, t.Name())
	t1 := time.Now().UTC().Truncate(time.Microsecond)
	t2 := t1.Add(time.Second)

	batch := []IngestEvent{
		{ID: "batch-evt-a", Type: "t", Subject: "s", Time: &t1},
		{ID: "batch-evt-b", Type: "t", Subject: "s", Time: &t2},
		{ID: "batch-evt-a", Type: "t", Subject: "s", Time: &t1}, // same as first
	}

	rec, dup, err := es.IngestEvents(ctx, orgID, batch)
	if err != nil {
		t.Fatalf("batch ingest: %v", err)
	}
	if rec != 2 || dup != 1 {
		t.Errorf("want received=2 dup=1, got received=%d dup=%d", rec, dup)
	}

	// Confirm two distinct rows.
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM events WHERE org_id=$1 AND id=ANY(ARRAY['batch-evt-a','batch-evt-b'])`, orgID).Scan(&count); err != nil {
		t.Fatalf("count scan: %v", err)
	}
	if count != 2 {
		t.Errorf("row count: want 2, got %d", count)
	}
}

// ── LATE EVENT (mandatory) ────────────────────────────────────────────────────

// An event with an old `time` must be stored with that time, not ingested_at.
func TestEventStore_LateEvent(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	es := NewTimescaleEventStore(pool)
	ctx := context.Background()

	orgID := createTestOrg(t, os, t.Name())

	lateTime := time.Now().UTC().Add(-7 * 24 * time.Hour).Truncate(time.Microsecond)
	evt := IngestEvent{ID: "late-evt-1", Type: "api_req", Subject: "user_2", Time: &lateTime}

	before := time.Now().UTC()
	_, _, err := es.IngestEvents(ctx, orgID, []IngestEvent{evt})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	var storedTime, ingestedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT time, ingested_at FROM events WHERE org_id=$1 AND id=$2`, orgID, "late-evt-1").
		Scan(&storedTime, &ingestedAt); err != nil {
		t.Fatalf("scan late event: %v", err)
	}

	// Event time must equal the caller-supplied late time.
	if !storedTime.Equal(lateTime) {
		t.Errorf("stored time: want %v, got %v", lateTime, storedTime)
	}
	// ingested_at must be close to now, not the late time.
	if ingestedAt.Before(before.Add(-time.Second)) {
		t.Errorf("ingested_at should be near now(), got %v", ingestedAt)
	}
	if ingestedAt.Sub(lateTime) < 6*24*time.Hour {
		t.Errorf("ingested_at (%v) should be much later than event time (%v)", ingestedAt, lateTime)
	}
}

// ── UNKNOWN TYPE (mandatory) ──────────────────────────────────────────────────

// An event whose type matches no meter must still be stored (store-and-flag policy).
func TestEventStore_UnknownType(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	es := NewTimescaleEventStore(pool)
	ctx := context.Background()

	orgID := createTestOrg(t, os, t.Name())
	// Deliberately use a type that no meter references.
	evt := IngestEvent{ID: "unknown-type-evt-1", Type: "no_such_meter_type_xyz", Subject: "user_3"}

	rec, dup, err := es.IngestEvents(ctx, orgID, []IngestEvent{evt})
	if err != nil {
		t.Fatalf("ingest unknown type: %v", err)
	}
	if rec != 1 || dup != 0 {
		t.Errorf("want received=1 dup=0, got received=%d dup=%d", rec, dup)
	}

	// Confirm the event is in the table.
	var storedType string
	if err := pool.QueryRow(ctx, `SELECT type FROM events WHERE org_id=$1 AND id=$2`, orgID, "unknown-type-evt-1").Scan(&storedType); err != nil {
		t.Fatalf("scan stored type: %v", err)
	}
	if storedType != "no_such_meter_type_xyz" {
		t.Errorf("stored type: want no_such_meter_type_xyz, got %q", storedType)
	}
}

// ── CROSS-ORG ISOLATION ───────────────────────────────────────────────────────

// An event inserted for org A must not appear in org B's dedup check.
func TestEventStore_TenancyIsolation(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	es := NewTimescaleEventStore(pool)
	ctx := context.Background()

	orgA := createTestOrg(t, os, t.Name()+"A")
	orgB := createTestOrg(t, os, t.Name()+"B")

	sharedID := "shared-evt-iso"
	tt := time.Now().UTC().Truncate(time.Microsecond)
	evt := IngestEvent{ID: sharedID, Type: "t", Subject: "s", Time: &tt}

	if _, _, err := es.IngestEvents(ctx, orgA, []IngestEvent{evt}); err != nil {
		t.Fatalf("ingest org A: %v", err)
	}
	// Same id for org B must not be counted as a duplicate.
	rec, dup, err := es.IngestEvents(ctx, orgB, []IngestEvent{evt})
	if err != nil {
		t.Fatalf("ingest org B: %v", err)
	}
	if rec != 1 || dup != 0 {
		t.Errorf("org B: want received=1 dup=0, got received=%d dup=%d", rec, dup)
	}
}
