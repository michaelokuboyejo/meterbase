package store

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// ── ListEvents ────────────────────────────────────────────────────────────────

func TestEventStore_ListEvents_Pagination(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	es := NewTimescaleEventStore(pool)
	ctx := context.Background()

	orgID := createTestOrg(t, os, t.Name())
	base := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)

	// Insert 5 events with distinct times.
	for i := 0; i < 5; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		_, _, err := es.IngestEvents(ctx, orgID, []IngestEvent{
			{ID: fmt.Sprintf("list-pg-%d", i), Type: "t", Subject: "s", Time: &ts},
		})
		if err != nil {
			t.Fatalf("ingest %d: %v", i, err)
		}
	}

	p := ListEventsParams{Limit: 3}
	page1, cursor1, err := es.ListEvents(ctx, orgID, p)
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1) != 3 {
		t.Errorf("page1 len: want 3, got %d", len(page1))
	}
	if cursor1 == "" {
		t.Error("page1: expected a nextCursor")
	}

	p2 := ListEventsParams{Limit: 3, Cursor: cursor1}
	page2, cursor2, err := es.ListEvents(ctx, orgID, p2)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2) != 2 {
		t.Errorf("page2 len: want 2, got %d", len(page2))
	}
	if cursor2 != "" {
		t.Errorf("page2: expected no nextCursor, got %q", cursor2)
	}

	// No id overlap between pages.
	seen := make(map[string]bool)
	for _, e := range page1 {
		seen[e.ID] = true
	}
	for _, e := range page2 {
		if seen[e.ID] {
			t.Errorf("duplicate event id %q across pages", e.ID)
		}
	}
}

func TestEventStore_ListEvents_SubjectFilter(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	es := NewTimescaleEventStore(pool)
	ctx := context.Background()

	orgID := createTestOrg(t, os, t.Name())
	base := time.Date(2026, 4, 2, 10, 0, 0, 0, time.UTC)

	for i := 0; i < 3; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		_, _, err := es.IngestEvents(ctx, orgID, []IngestEvent{
			{ID: fmt.Sprintf("subj-a-%d", i), Type: "t", Subject: "user_a", Time: &ts},
		})
		if err != nil {
			t.Fatalf("ingest user_a %d: %v", i, err)
		}
	}
	for i := 0; i < 2; i++ {
		ts := base.Add(time.Duration(10+i) * time.Minute)
		_, _, err := es.IngestEvents(ctx, orgID, []IngestEvent{
			{ID: fmt.Sprintf("subj-b-%d", i), Type: "t", Subject: "user_b", Time: &ts},
		})
		if err != nil {
			t.Fatalf("ingest user_b %d: %v", i, err)
		}
	}

	p := ListEventsParams{Subject: "user_a", Limit: 100}
	events, _, err := es.ListEvents(ctx, orgID, p)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 3 {
		t.Errorf("want 3 events for user_a, got %d", len(events))
	}
	for _, e := range events {
		if e.Subject != "user_a" {
			t.Errorf("unexpected subject %q in filtered results", e.Subject)
		}
	}
}

func TestEventStore_ListEvents_TypeFilter(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	es := NewTimescaleEventStore(pool)
	ctx := context.Background()

	orgID := createTestOrg(t, os, t.Name())
	base := time.Date(2026, 4, 3, 9, 0, 0, 0, time.UTC)

	for i := 0; i < 2; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		_, _, err := es.IngestEvents(ctx, orgID, []IngestEvent{
			{ID: fmt.Sprintf("type-a-%d", i), Type: "type_alpha", Subject: "s", Time: &ts},
		})
		if err != nil {
			t.Fatalf("ingest alpha: %v", err)
		}
	}
	ts := base.Add(10 * time.Minute)
	_, _, err := es.IngestEvents(ctx, orgID, []IngestEvent{
		{ID: "type-b-0", Type: "type_beta", Subject: "s", Time: &ts},
	})
	if err != nil {
		t.Fatalf("ingest beta: %v", err)
	}

	p := ListEventsParams{Type: "type_alpha", Limit: 100}
	events, _, err := es.ListEvents(ctx, orgID, p)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("want 2 events for type_alpha, got %d", len(events))
	}
}

// ── ExportEvents ──────────────────────────────────────────────────────────────

func TestEventStore_ExportEvents(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	es := NewTimescaleEventStore(pool)
	ctx := context.Background()

	orgID := createTestOrg(t, os, t.Name())
	base := time.Date(2026, 4, 4, 8, 0, 0, 0, time.UTC)

	for i := 0; i < 4; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		data, _ := json.Marshal(map[string]int{"n": i})
		_, _, err := es.IngestEvents(ctx, orgID, []IngestEvent{
			{ID: fmt.Sprintf("exp-%d", i), Type: "exp-type", Subject: "s", Time: &ts, Data: data},
		})
		if err != nil {
			t.Fatalf("ingest %d: %v", i, err)
		}
	}

	p := ListEventsParams{Type: "exp-type"}
	events, err := es.ExportEvents(ctx, orgID, p)
	if err != nil {
		t.Fatalf("ExportEvents: %v", err)
	}
	if len(events) != 4 {
		t.Errorf("want 4 exported events, got %d", len(events))
	}
	// Data should be preserved.
	for _, e := range events {
		var d map[string]int
		if err := json.Unmarshal(e.Data, &d); err != nil {
			t.Errorf("data unmarshal: %v", err)
		}
	}
}

// TenancyIsolation: listing events for org A must not return org B's events.
func TestEventStore_ListEvents_TenancyIsolation(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	es := NewTimescaleEventStore(pool)
	ctx := context.Background()

	orgA := createTestOrg(t, os, t.Name()+"A")
	orgB := createTestOrg(t, os, t.Name()+"B")

	ts := time.Now().UTC().Truncate(time.Microsecond)
	_, _, err := es.IngestEvents(ctx, orgA, []IngestEvent{
		{ID: "iso-list-1", Type: "t", Subject: "s", Time: &ts},
	})
	if err != nil {
		t.Fatalf("ingest orgA: %v", err)
	}

	events, _, err := es.ListEvents(ctx, orgB, ListEventsParams{Limit: 100})
	if err != nil {
		t.Fatalf("ListEvents orgB: %v", err)
	}
	for _, e := range events {
		if e.ID == "iso-list-1" {
			t.Error("org B should not see org A's events")
		}
	}
}
