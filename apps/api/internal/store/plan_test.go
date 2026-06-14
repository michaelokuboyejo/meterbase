package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestPlanStore_CreatePlan(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	ps := NewPlanStore(pool)
	ctx := context.Background()

	orgID := createTestOrg(t, os, t.Name())

	plan, err := ps.CreatePlan(ctx, orgID, "Test Plan")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if plan.ID == "" {
		t.Error("plan.ID should not be empty")
	}
	if plan.Name != "Test Plan" {
		t.Errorf("plan.Name: want %q, got %q", "Test Plan", plan.Name)
	}
	if plan.OrgID != orgID {
		t.Errorf("plan.OrgID: want %q, got %q", orgID, plan.OrgID)
	}
}

func TestPlanStore_GetPlan(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	ps := NewPlanStore(pool)
	ctx := context.Background()

	orgA := createTestOrg(t, os, t.Name()+"A")
	orgB := createTestOrg(t, os, t.Name()+"B")

	plan, err := ps.CreatePlan(ctx, orgA, "Org A Plan")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	// Correct org can fetch it.
	got, err := ps.GetPlan(ctx, orgA, plan.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.ID != plan.ID {
		t.Errorf("got wrong plan id: %s", got.ID)
	}

	// Different org returns ErrPlanNotFound — tenancy enforced.
	_, err = ps.GetPlan(ctx, orgB, plan.ID)
	if !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("expected ErrPlanNotFound for wrong org, got %v", err)
	}

	// Non-existent ID returns ErrPlanNotFound.
	_, err = ps.GetPlan(ctx, orgA, "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("expected ErrPlanNotFound for missing id, got %v", err)
	}
}

func TestPlanStore_CreateRateCard(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	ps := NewPlanStore(pool)
	ms := NewMeterStore(pool)
	ctx := context.Background()

	orgID := createTestOrg(t, os, t.Name())

	plan, err := ps.CreatePlan(ctx, orgID, "RC Plan")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	meter, err := ms.CreateMeter(ctx, orgID, "rc-meter", "evt", "COUNT", nil, nil)
	if err != nil {
		t.Fatalf("CreateMeter: %v", err)
	}

	cfg := json.RawMessage(`{"unitPrice":0.01}`)
	rc, err := ps.CreateRateCard(ctx, plan.ID, meter.ID, "PAYG", cfg, "USD")
	if err != nil {
		t.Fatalf("CreateRateCard: %v", err)
	}
	if rc.ID == "" {
		t.Error("rate card ID should not be empty")
	}
	if rc.PlanID != plan.ID {
		t.Errorf("PlanID: want %s, got %s", plan.ID, rc.PlanID)
	}
	if rc.Model != "PAYG" {
		t.Errorf("Model: want PAYG, got %s", rc.Model)
	}
}

func TestPlanStore_ListRateCardsByPlan(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	ps := NewPlanStore(pool)
	ms := NewMeterStore(pool)
	ctx := context.Background()

	orgA := createTestOrg(t, os, t.Name()+"A")
	orgB := createTestOrg(t, os, t.Name()+"B")

	plan, _ := ps.CreatePlan(ctx, orgA, "List Test Plan")
	meter, _ := ms.CreateMeter(ctx, orgA, "list-rc-meter", "list_evt", "COUNT", nil, nil)

	cfg := json.RawMessage(`{"unitPrice":0.005}`)
	_, err := ps.CreateRateCard(ctx, plan.ID, meter.ID, "PAYG", cfg, "USD")
	if err != nil {
		t.Fatalf("CreateRateCard: %v", err)
	}

	// Correct org sees the rate card with joined meter metadata.
	rcs, err := ps.ListRateCardsByPlan(ctx, orgA, plan.ID)
	if err != nil {
		t.Fatalf("ListRateCardsByPlan: %v", err)
	}
	if len(rcs) != 1 {
		t.Fatalf("expected 1 rate card, got %d", len(rcs))
	}
	if rcs[0].MeterSlug != "list-rc-meter" {
		t.Errorf("MeterSlug: want list-rc-meter, got %s", rcs[0].MeterSlug)
	}
	if rcs[0].MeterEventType != "list_evt" {
		t.Errorf("MeterEventType: want list_evt, got %s", rcs[0].MeterEventType)
	}

	// Different org sees nothing — tenancy enforced via JOIN through plans.org_id.
	rcsB, err := ps.ListRateCardsByPlan(ctx, orgB, plan.ID)
	if err != nil {
		t.Fatalf("ListRateCardsByPlan orgB: %v", err)
	}
	if len(rcsB) != 0 {
		t.Errorf("expected 0 rate cards for wrong org, got %d", len(rcsB))
	}
}
