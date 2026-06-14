package store

import (
	"context"
	"errors"
	"testing"
)

func TestMeterStore_Create(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	ms := NewMeterStore(pool)
	ctx := context.Background()

	orgID := createTestOrg(t, os, t.Name())

	m, err := ms.CreateMeter(ctx, orgID, "api_requests", "api_request", "SUM", ptrStr("tokens"), []string{"model"})
	if err != nil {
		t.Fatalf("CreateMeter: %v", err)
	}
	if m.ID == "" {
		t.Error("ID should not be empty")
	}
	if m.Slug != "api_requests" {
		t.Errorf("Slug: want api_requests, got %s", m.Slug)
	}
	if m.Aggregation != "SUM" {
		t.Errorf("Aggregation: want SUM, got %s", m.Aggregation)
	}
	if m.ValueProperty == nil || *m.ValueProperty != "tokens" {
		t.Errorf("ValueProperty: want tokens, got %v", m.ValueProperty)
	}
	if len(m.GroupBy) != 1 || m.GroupBy[0] != "model" {
		t.Errorf("GroupBy: want [model], got %v", m.GroupBy)
	}
}

func TestMeterStore_CreateDefaultGroupBy(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	ms := NewMeterStore(pool)
	ctx := context.Background()

	orgID := createTestOrg(t, os, t.Name())
	m, err := ms.CreateMeter(ctx, orgID, "counts", "click", "COUNT", nil, nil)
	if err != nil {
		t.Fatalf("CreateMeter: %v", err)
	}
	if m.GroupBy == nil || len(m.GroupBy) != 0 {
		t.Errorf("GroupBy: want [], got %v", m.GroupBy)
	}
}

// DoD: duplicate slug in the same org returns ErrDuplicateMeterSlug.
func TestMeterStore_DuplicateSlug_SameOrg(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	ms := NewMeterStore(pool)
	ctx := context.Background()

	orgID := createTestOrg(t, os, t.Name())
	if _, err := ms.CreateMeter(ctx, orgID, "dup-slug", "t", "COUNT", nil, nil); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := ms.CreateMeter(ctx, orgID, "dup-slug", "t", "COUNT", nil, nil)
	if !errors.Is(err, ErrDuplicateMeterSlug) {
		t.Errorf("want ErrDuplicateMeterSlug, got %v", err)
	}
}

// Same slug in a different org is allowed.
func TestMeterStore_DuplicateSlug_DifferentOrg(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	ms := NewMeterStore(pool)
	ctx := context.Background()

	orgA := createTestOrg(t, os, t.Name()+"A")
	orgB := createTestOrg(t, os, t.Name()+"B")

	if _, err := ms.CreateMeter(ctx, orgA, "shared-slug", "t", "COUNT", nil, nil); err != nil {
		t.Fatalf("create org A: %v", err)
	}
	if _, err := ms.CreateMeter(ctx, orgB, "shared-slug", "t", "COUNT", nil, nil); err != nil {
		t.Errorf("create same slug in org B: want nil error, got %v", err)
	}
}

func TestMeterStore_GetBySlug(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	ms := NewMeterStore(pool)
	ctx := context.Background()

	orgID := createTestOrg(t, os, t.Name())
	created, err := ms.CreateMeter(ctx, orgID, "get-slug", "evt", "AVG", ptrStr("latency"), nil)
	if err != nil {
		t.Fatalf("CreateMeter: %v", err)
	}

	got, err := ms.GetMeterBySlug(ctx, orgID, "get-slug")
	if err != nil {
		t.Fatalf("GetMeterBySlug: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID mismatch")
	}
	if got.EventType != "evt" {
		t.Errorf("EventType: want evt, got %s", got.EventType)
	}
}

func TestMeterStore_GetBySlug_NotFound(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	ms := NewMeterStore(pool)
	ctx := context.Background()

	orgID := createTestOrg(t, os, t.Name())
	_, err := ms.GetMeterBySlug(ctx, orgID, "no-such-slug")
	if !errors.Is(err, ErrMeterNotFound) {
		t.Errorf("want ErrMeterNotFound, got %v", err)
	}
}

// GetBySlug scoped to org: org B cannot see org A's meter.
func TestMeterStore_GetBySlug_CrossOrg(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	ms := NewMeterStore(pool)
	ctx := context.Background()

	orgA := createTestOrg(t, os, t.Name()+"A")
	orgB := createTestOrg(t, os, t.Name()+"B")

	if _, err := ms.CreateMeter(ctx, orgA, "cross-slug", "t", "COUNT", nil, nil); err != nil {
		t.Fatalf("CreateMeter: %v", err)
	}
	_, err := ms.GetMeterBySlug(ctx, orgB, "cross-slug")
	if !errors.Is(err, ErrMeterNotFound) {
		t.Errorf("cross-org get: want ErrMeterNotFound, got %v", err)
	}
}

func TestMeterStore_List(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	ms := NewMeterStore(pool)
	ctx := context.Background()

	orgID := createTestOrg(t, os, t.Name())
	for _, slug := range []string{"m-list-a", "m-list-b", "m-list-c"} {
		if _, err := ms.CreateMeter(ctx, orgID, slug, "t", "COUNT", nil, nil); err != nil {
			t.Fatalf("CreateMeter %s: %v", slug, err)
		}
	}

	meters, next, err := ms.ListMeters(ctx, orgID, 100, "")
	if err != nil {
		t.Fatalf("ListMeters: %v", err)
	}
	if len(meters) < 3 {
		t.Errorf("want ≥3 meters, got %d", len(meters))
	}
	_ = next
	for _, m := range meters {
		if m.OrgID != orgID {
			t.Errorf("ListMeters returned meter from wrong org: %s", m.OrgID)
		}
	}
}

func TestMeterStore_Delete(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	ms := NewMeterStore(pool)
	ctx := context.Background()

	orgID := createTestOrg(t, os, t.Name())
	if _, err := ms.CreateMeter(ctx, orgID, "del-slug", "t", "COUNT", nil, nil); err != nil {
		t.Fatalf("CreateMeter: %v", err)
	}

	if err := ms.DeleteMeter(ctx, orgID, "del-slug"); err != nil {
		t.Fatalf("DeleteMeter: %v", err)
	}

	_, err := ms.GetMeterBySlug(ctx, orgID, "del-slug")
	if !errors.Is(err, ErrMeterNotFound) {
		t.Errorf("after delete: want ErrMeterNotFound, got %v", err)
	}
}

func TestMeterStore_DeleteNotFound(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	ms := NewMeterStore(pool)
	ctx := context.Background()

	orgID := createTestOrg(t, os, t.Name())
	err := ms.DeleteMeter(ctx, orgID, "ghost-slug")
	if !errors.Is(err, ErrMeterNotFound) {
		t.Errorf("want ErrMeterNotFound, got %v", err)
	}
}
