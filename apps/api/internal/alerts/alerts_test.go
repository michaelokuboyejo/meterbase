package alerts_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/alerts"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/store"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/usage"
)

func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := store.NewPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("getTestPool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// setupEvaluatorFixture creates the full set of fixtures needed for evaluator tests:
// org → API key (so the org exists with a key) → meter (COUNT) → alert rule →
// webhook endpoint. Returns the org ID and endpoint ID.
func setupEvaluatorFixture(t *testing.T, pool *pgxpool.Pool, label string) (orgID, endpointID string) {
	t.Helper()
	ctx := context.Background()

	orgStore := store.NewOrgStore(pool)
	meterStore := store.NewMeterStore(pool)
	alertStore := store.NewAlertStore(pool)

	org, err := orgStore.CreateOrg(ctx, "eval-org-"+label)
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}

	// Create a COUNT meter (no value_prop needed).
	meter, err := meterStore.CreateMeter(ctx, org.ID, "evt_count", "test_event", "COUNT", nil, nil)
	if err != nil {
		t.Fatalf("CreateMeter: %v", err)
	}

	// Alert rule: global scope, HOUR window, threshold=5.
	rule, err := alertStore.CreateAlertRule(ctx, org.ID, store.AlertRuleInput{
		MeterID:   meter.ID,
		Scope:     "global",
		Window:    "HOUR",
		Threshold: 5,
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("CreateAlertRule: %v", err)
	}
	_ = rule

	ep, err := alertStore.CreateWebhookEndpoint(ctx, org.ID, "http://unused.example", "testsecret")
	if err != nil {
		t.Fatalf("CreateWebhookEndpoint: %v", err)
	}
	return org.ID, ep.ID
}

// ingestEvents inserts n events of type "test_event" for orgID at the given event time.
func ingestEvents(t *testing.T, pool *pgxpool.Pool, orgID string, n int, at time.Time) {
	t.Helper()
	es := store.NewTimescaleEventStore(pool)
	ctx := context.Background()
	for i := 0; i < n; i++ {
		evts := []store.IngestEvent{{
			ID:      fmt.Sprintf("evt-%s-%d", t.Name(), i),
			Type:    "test_event",
			Subject: "sub_1",
			Time:    &at,
		}}
		if _, _, err := es.IngestEvents(ctx, orgID, evts); err != nil {
			t.Fatalf("IngestEvents[%d]: %v", i, err)
		}
	}
}

// countDeliveries returns the number of deliveries for endpointID.
func countDeliveries(t *testing.T, pool *pgxpool.Pool, endpointID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM webhook_deliveries WHERE endpoint_id = $1::uuid",
		endpointID,
	).Scan(&n); err != nil {
		t.Fatalf("countDeliveries: %v", err)
	}
	return n
}

// TestEvaluator_FireOnce verifies that crossing a threshold fires exactly one delivery,
// and a second tick within the same window does NOT fire again (de-bounce).
func TestEvaluator_FireOnce(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	orgID, epID := setupEvaluatorFixture(t, pool, t.Name())

	alertStore := store.NewAlertStore(pool)
	usageRepo := usage.NewTimescaleUsageRepository(pool)
	ev := alerts.NewEvaluator(alertStore, usageRepo)

	// Define the evaluation "now": one minute in the future so all events ingested
	// right now fall in [windowStart, now) under an HOUR window.
	now := time.Now().UTC().Add(time.Minute)
	eventTime := now.Add(-30 * time.Second) // inside [windowStart, now)

	// Ingest 10 events (COUNT=10 > threshold=5).
	ingestEvents(t, pool, orgID, 10, eventTime)

	// --- First tick: should fire exactly once. ---
	ev.Tick(ctx, now)
	if got := countDeliveries(t, pool, epID); got != 1 {
		t.Errorf("after first tick: want 1 delivery, got %d", got)
	}

	// --- Second tick (same window, still above threshold): must NOT re-fire. ---
	ev.Tick(ctx, now)
	if got := countDeliveries(t, pool, epID); got != 1 {
		t.Errorf("after second tick (same window): want still 1 delivery (de-bounce), got %d", got)
	}
}

// TestEvaluator_BelowThreshold verifies that no delivery is created when usage < threshold.
func TestEvaluator_BelowThreshold(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	orgID, epID := setupEvaluatorFixture(t, pool, t.Name())

	alertStore := store.NewAlertStore(pool)
	usageRepo := usage.NewTimescaleUsageRepository(pool)
	ev := alerts.NewEvaluator(alertStore, usageRepo)

	now := time.Now().UTC().Add(time.Minute)
	eventTime := now.Add(-30 * time.Second)

	// Ingest 3 events (COUNT=3 < threshold=5): no delivery should be created.
	ingestEvents(t, pool, orgID, 3, eventTime)

	ev.Tick(ctx, now)
	if got := countDeliveries(t, pool, epID); got != 0 {
		t.Errorf("below threshold: want 0 deliveries, got %d", got)
	}
}

// TestEvaluator_SubjectScope verifies that the evaluator fires per distinct subject
// that exceeds the threshold when scope=subject.
func TestEvaluator_SubjectScope(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	orgStore := store.NewOrgStore(pool)
	meterStore := store.NewMeterStore(pool)
	alertStore := store.NewAlertStore(pool)

	org, err := orgStore.CreateOrg(ctx, "eval-subject-"+t.Name())
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
	meter, err := meterStore.CreateMeter(ctx, org.ID, "subject_count", "subject_event", "COUNT", nil, nil)
	if err != nil {
		t.Fatalf("CreateMeter: %v", err)
	}
	_, err = alertStore.CreateAlertRule(ctx, org.ID, store.AlertRuleInput{
		MeterID:   meter.ID,
		Scope:     "subject",
		Window:    "HOUR",
		Threshold: 3,
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("CreateAlertRule: %v", err)
	}
	ep, err := alertStore.CreateWebhookEndpoint(ctx, org.ID, "http://unused.example", "testsecret")
	if err != nil {
		t.Fatalf("CreateWebhookEndpoint: %v", err)
	}

	usageRepo := usage.NewTimescaleUsageRepository(pool)
	ev := alerts.NewEvaluator(alertStore, usageRepo)

	now := time.Now().UTC().Add(time.Minute)
	evtTime := now.Add(-30 * time.Second)

	es := store.NewTimescaleEventStore(pool)
	// sub_A: 5 events (exceeds threshold=3)
	for i := 0; i < 5; i++ {
		if _, _, err := es.IngestEvents(ctx, org.ID, []store.IngestEvent{{
			ID: fmt.Sprintf("subj-a-%s-%d", t.Name(), i), Type: "subject_event",
			Subject: "sub_A", Time: &evtTime,
		}}); err != nil {
			t.Fatalf("IngestEvents sub_A[%d]: %v", i, err)
		}
	}
	// sub_B: 1 event (below threshold=3)
	if _, _, err := es.IngestEvents(ctx, org.ID, []store.IngestEvent{{
		ID: "subj-b-" + t.Name(), Type: "subject_event",
		Subject: "sub_B", Time: &evtTime,
	}}); err != nil {
		t.Fatalf("IngestEvents sub_B: %v", err)
	}

	ev.Tick(ctx, now)

	// Only sub_A should fire → 1 delivery.
	if got := countDeliveries(t, pool, ep.ID); got != 1 {
		t.Errorf("subject scope: want 1 delivery (sub_A only), got %d", got)
	}
}
