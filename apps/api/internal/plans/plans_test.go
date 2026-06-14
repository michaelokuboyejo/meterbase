package plans_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/pricing"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/store"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/usage"
)

// ── test helpers ──────────────────────────────────────────────────────────────

func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := store.NewPool(ctx, url)
	if err != nil {
		t.Fatalf("getTestPool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func createTestOrg(t *testing.T, os *store.OrgStore, name string) string {
	t.Helper()
	org, err := os.CreateOrg(context.Background(), name)
	if err != nil {
		t.Fatalf("createTestOrg: %v", err)
	}
	return org.ID
}

// ── REPRODUCIBILITY (mandatory DoD test) ──────────────────────────────────────

// TestReproducibility_TotalUsage verifies that querying total usage twice over
// the same raw events always returns the same value — the foundation of
// deterministic cost computation.
func TestReproducibility_TotalUsage(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	os := store.NewOrgStore(pool)
	cs := store.NewCustomerStore(pool)
	ms := store.NewMeterStore(pool)
	es := store.NewTimescaleEventStore(pool)
	usageRepo := usage.NewTimescaleUsageRepository(pool)

	orgID := createTestOrg(t, os, t.Name())

	// Create customer with external_id "cust-repro".
	_, err := cs.CreateCustomer(ctx, orgID, "cust-repro", nil, nil)
	if err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}

	// Create a SUM meter on "tokens".
	valueProp := "tokens"
	meter, err := ms.CreateMeter(ctx, orgID, "repro-tokens", "repro_evt", "SUM", &valueProp, nil)
	if err != nil {
		t.Fatalf("CreateMeter: %v", err)
	}
	_ = meter

	// Ingest 3 events for subject="cust-repro" with tokens 500, 300, 200 = total 1000.
	now := time.Now().UTC()
	events := []store.IngestEvent{
		{ID: "repro-1", Type: "repro_evt", Subject: "cust-repro", Time: &now, Data: json.RawMessage(`{"tokens":500}`)},
		{ID: "repro-2", Type: "repro_evt", Subject: "cust-repro", Time: &now, Data: json.RawMessage(`{"tokens":300}`)},
		{ID: "repro-3", Type: "repro_evt", Subject: "cust-repro", Time: &now, Data: json.RawMessage(`{"tokens":200}`)},
	}
	rec, dup, err := es.IngestEvents(ctx, orgID, events)
	if err != nil {
		t.Fatalf("IngestEvents: %v", err)
	}
	if rec != 3 || dup != 0 {
		t.Fatalf("want rec=3 dup=0, got rec=%d dup=%d", rec, dup)
	}

	from := now.Add(-time.Minute)
	to := now.Add(time.Minute)
	params := usage.QueryParams{
		OrgID:      orgID,
		MeterType:  "repro_evt",
		Agg:        "SUM",
		ValueProp:  &valueProp,
		Subject:    "cust-repro",
		From:       from,
		To:         to,
		WindowSize: "DAY", // not used by TotalUsage but kept for completeness
	}

	// Call TotalUsage twice — both must return 1000.
	first, err := usageRepo.TotalUsage(ctx, params)
	if err != nil {
		t.Fatalf("TotalUsage first: %v", err)
	}
	second, err := usageRepo.TotalUsage(ctx, params)
	if err != nil {
		t.Fatalf("TotalUsage second: %v", err)
	}
	if first != second {
		t.Errorf("TotalUsage not deterministic: first=%v second=%v", first, second)
	}
	if first != 1000 {
		t.Errorf("TotalUsage: want 1000, got %v", first)
	}

	// ComputeCost on the same usage twice must also be identical.
	cfg, _ := json.Marshal(pricing.PAYGConfig{UnitPrice: 0.001})
	cost1, err := pricing.ComputeCost("PAYG", cfg, first)
	if err != nil {
		t.Fatalf("ComputeCost first: %v", err)
	}
	cost2, err := pricing.ComputeCost("PAYG", cfg, second)
	if err != nil {
		t.Fatalf("ComputeCost second: %v", err)
	}
	if cost1 != cost2 {
		t.Errorf("ComputeCost not deterministic: cost1=%v cost2=%v", cost1, cost2)
	}
	if cost1 != 1.00 {
		t.Errorf("ComputeCost: want 1.00, got %v", cost1)
	}
}

// TestTotalUsage_AllModels verifies TotalUsage returns correct totals for each
// aggregation type, matching a brute-force count over the inserted events.
func TestTotalUsage_AllModels(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	os := store.NewOrgStore(pool)
	ms := store.NewMeterStore(pool)
	es := store.NewTimescaleEventStore(pool)
	usageRepo := usage.NewTimescaleUsageRepository(pool)

	orgID := createTestOrg(t, os, t.Name())
	now := time.Now().UTC()

	// Ingest 4 events: tokens = 10, 20, 30, 40 (total=100, avg=25, min=10, max=40)
	events := []store.IngestEvent{
		{ID: "agg-1", Type: "agg_evt", Subject: "s", Time: &now, Data: json.RawMessage(`{"tokens":10}`)},
		{ID: "agg-2", Type: "agg_evt", Subject: "s", Time: &now, Data: json.RawMessage(`{"tokens":20}`)},
		{ID: "agg-3", Type: "agg_evt", Subject: "s", Time: &now, Data: json.RawMessage(`{"tokens":30}`)},
		{ID: "agg-4", Type: "agg_evt", Subject: "s", Time: &now, Data: json.RawMessage(`{"tokens":40}`)},
	}
	if _, _, err := es.IngestEvents(ctx, orgID, events); err != nil {
		t.Fatalf("IngestEvents: %v", err)
	}

	valueProp := "tokens"
	from := now.Add(-time.Minute)
	to := now.Add(time.Minute)

	// Meter is needed for SUM/AVG/MIN/MAX to have a valueProp
	_, err := ms.CreateMeter(ctx, orgID, "agg-total-meter", "agg_evt", "SUM", &valueProp, nil)
	if err != nil {
		t.Fatalf("CreateMeter: %v", err)
	}

	cases := []struct {
		agg  string
		want float64
	}{
		{"COUNT", 4},
		{"SUM", 100},
		{"AVG", 25},
		{"MIN", 10},
		{"MAX", 40},
	}

	for _, tc := range cases {
		t.Run(tc.agg, func(t *testing.T) {
			var vp *string
			if tc.agg != "COUNT" {
				vp = &valueProp
			}
			params := usage.QueryParams{
				OrgID:      orgID,
				MeterType:  "agg_evt",
				Agg:        tc.agg,
				ValueProp:  vp,
				Subject:    "s",
				From:       from,
				To:         to,
				WindowSize: "DAY",
			}
			got, err := usageRepo.TotalUsage(ctx, params)
			if err != nil {
				t.Fatalf("TotalUsage %s: %v", tc.agg, err)
			}
			if got != tc.want {
				t.Errorf("%s: want %v, got %v", tc.agg, tc.want, got)
			}
		})
	}
}

// TestPlanStore_RateCardTenancy verifies that rate cards from a plan belonging
// to org A are invisible when listed with org B's ID.
func TestPlanStore_RateCardTenancy(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	os := store.NewOrgStore(pool)
	ms := store.NewMeterStore(pool)
	ps := store.NewPlanStore(pool)

	orgA := createTestOrg(t, os, t.Name()+"A")
	orgB := createTestOrg(t, os, t.Name()+"B")

	plan, _ := ps.CreatePlan(ctx, orgA, "Tenancy Plan")
	meter, _ := ms.CreateMeter(ctx, orgA, "tenancy-meter", "evt", "COUNT", nil, nil)
	_, err := ps.CreateRateCard(ctx, plan.ID, meter.ID, "PAYG", json.RawMessage(`{"unitPrice":0.01}`), "USD")
	if err != nil {
		t.Fatalf("CreateRateCard: %v", err)
	}

	// GetPlan with org B returns ErrPlanNotFound.
	_, err = ps.GetPlan(ctx, orgB, plan.ID)
	if !errors.Is(err, store.ErrPlanNotFound) {
		t.Errorf("GetPlan wrong org: expected ErrPlanNotFound, got %v", err)
	}

	// ListRateCardsByPlan with org B returns empty (not an error).
	rcs, err := ps.ListRateCardsByPlan(ctx, orgB, plan.ID)
	if err != nil {
		t.Fatalf("ListRateCardsByPlan orgB: %v", err)
	}
	if len(rcs) != 0 {
		t.Errorf("expected 0 rate cards for wrong org, got %d", len(rcs))
	}
}
