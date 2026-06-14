package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/store"
)

// ── test helpers ─────────────────────────────────────────────────────────────

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

func ptrStr(s string) *string { return &s }

// refreshAggregates forces a full refresh of ALL continuous aggregate views,
// including per-meter ones created at meter-creation time.
func refreshAggregates(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	rows, err := pool.Query(ctx,
		`SELECT view_name FROM timescaledb_information.continuous_aggregates ORDER BY view_name`,
	)
	if err != nil {
		t.Fatalf("list continuous aggregates: %v", err)
	}
	var views []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			t.Fatalf("scan view name: %v", err)
		}
		views = append(views, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("continuous_aggregates rows: %v", err)
	}
	for _, view := range views {
		if _, err := pool.Exec(ctx,
			fmt.Sprintf("CALL refresh_continuous_aggregate('%s', NULL, NULL)", view),
		); err != nil {
			t.Fatalf("refresh %s: %v", view, err)
		}
	}
}

// ingestEvents seeds raw events directly via the store.
func ingestEvents(t *testing.T, pool *pgxpool.Pool, orgID string, events []store.IngestEvent) {
	t.Helper()
	es := store.NewTimescaleEventStore(pool)
	_, _, err := es.IngestEvents(context.Background(), orgID, events)
	if err != nil {
		t.Fatalf("ingestEvents: %v", err)
	}
}

// createMeter creates a meter via the store.
func createMeter(t *testing.T, pool *pgxpool.Pool, orgID, slug, eventType, agg string, valueProp *string, groupBy []string) *store.Meter {
	t.Helper()
	ms := store.NewMeterStore(pool)
	m, err := ms.CreateMeter(context.Background(), orgID, slug, eventType, agg, valueProp, groupBy)
	if err != nil {
		t.Fatalf("createMeter: %v", err)
	}
	return m
}

// bruteForce computes the expected aggregation directly over raw events (no rollup).
// Returns 0 when there are no matching rows (for MIN/MAX this is ambiguous but fine for tests).
func bruteForce(t *testing.T, pool *pgxpool.Pool, orgID, eventType, valueProp, agg string, from, to time.Time) float64 {
	t.Helper()
	ctx := context.Background()

	var expr string
	switch agg {
	case "COUNT":
		expr = "COUNT(*)"
	case "SUM":
		expr = fmt.Sprintf("COALESCE(SUM((data->>%s)::numeric), 0)", pgQuoteLiteral(valueProp))
	case "AVG":
		expr = fmt.Sprintf("COALESCE(AVG((data->>%s)::numeric), 0)", pgQuoteLiteral(valueProp))
	case "MIN":
		expr = fmt.Sprintf("COALESCE(MIN((data->>%s)::numeric), 0)", pgQuoteLiteral(valueProp))
	case "MAX":
		expr = fmt.Sprintf("COALESCE(MAX((data->>%s)::numeric), 0)", pgQuoteLiteral(valueProp))
	case "UNIQUE_COUNT":
		expr = fmt.Sprintf("COUNT(DISTINCT data->>%s)", pgQuoteLiteral(valueProp))
	default:
		t.Fatalf("unknown agg: %s", agg)
	}

	q := fmt.Sprintf(
		`SELECT COALESCE(%s, 0)::float8 FROM events WHERE org_id=$1 AND type=$2 AND time>=$3 AND time<$4`,
		expr,
	)
	var v float64
	if err := pool.QueryRow(ctx, q, orgID, eventType, from, to).Scan(&v); err != nil {
		t.Fatalf("bruteForce %s: %v", agg, err)
	}
	return v
}

func pgQuoteLiteral(s string) string {
	return "'" + s + "'"
}

func approxEqual(a, b float64) bool {
	if b == 0 {
		return math.Abs(a) < 1e-9
	}
	return math.Abs(a-b)/math.Abs(b) < 1e-9
}

// ── CORRECTNESS (mandatory) ───────────────────────────────────────────────────

// For each aggregation: seed events, query via UsageRepository, compare to brute-force.
func TestUsage_Correctness(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()
	os := store.NewOrgStore(pool)
	repo := NewTimescaleUsageRepository(pool)

	// A fixed bucket: 2026-01-15 14:xx UTC (past, in the materialized range).
	bucketStart := time.Date(2026, 1, 15, 14, 0, 0, 0, time.UTC)
	bucketEnd := bucketStart.Add(time.Hour)

	// Seed values: [10, 20, 30, 40, 50] → sum=150, avg=30, min=10, max=50, unique_count=5
	values := []float64{10, 20, 30, 40, 50}

	type testCase struct {
		agg       string
		valueProp *string
		want      float64
	}
	cases := []testCase{
		{"COUNT", nil, float64(len(values))},
		{"SUM", ptrStr("tokens"), 150},
		{"AVG", ptrStr("tokens"), 30},
		{"MIN", ptrStr("tokens"), 10},
		{"MAX", ptrStr("tokens"), 50},
		{"UNIQUE_COUNT", ptrStr("tokens"), float64(len(values))},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.agg, func(t *testing.T) {
			orgID := createTestOrg(t, os, "correctness-"+tc.agg)
			slug := "m-" + tc.agg

			m := createMeter(t, pool, orgID, slug, "evt-"+tc.agg, tc.agg, tc.valueProp, nil)

			// Seed events spread within the bucket, each with a unique id.
			evts := make([]store.IngestEvent, len(values))
			for i, v := range values {
				ts := bucketStart.Add(time.Duration(i) * time.Minute)
				data, _ := json.Marshal(map[string]float64{"tokens": v})
				evts[i] = store.IngestEvent{
					ID:      fmt.Sprintf("corr-%s-%d", tc.agg, i),
					Type:    "evt-" + tc.agg,
					Subject: "s1",
					Time:    &ts,
					Data:    data,
				}
			}
			ingestEvents(t, pool, orgID, evts)
			refreshAggregates(t, pool)

			result, err := repo.QueryUsage(ctx, QueryParams{
				OrgID:      orgID,
				MeterSlug:  slug,
				MeterID:    m.ID,
				MeterType:  "evt-" + tc.agg,
				Agg:        tc.agg,
				ValueProp:  tc.valueProp,
				From:       bucketStart,
				To:         bucketEnd,
				WindowSize: "HOUR",
			})
			if err != nil {
				t.Fatalf("QueryUsage: %v", err)
			}
			if len(result.Data) != 1 {
				t.Fatalf("want 1 data point, got %d", len(result.Data))
			}
			got := result.Data[0].Value

			// Also verify against brute-force.
			vp := ""
			if tc.valueProp != nil {
				vp = *tc.valueProp
			}
			bf := bruteForce(t, pool, orgID, "evt-"+tc.agg, vp, tc.agg, bucketStart, bucketEnd)

			if !approxEqual(got, tc.want) {
				t.Errorf("agg=%s: want %v, got %v", tc.agg, tc.want, got)
			}
			if !approxEqual(got, bf) {
				t.Errorf("agg=%s: rollup=%v != brute-force=%v", tc.agg, got, bf)
			}
		})
	}
}

// ── LATE EVENT (mandatory) ────────────────────────────────────────────────────

// After inserting a late event and refreshing, the historical bucket must reflect it.
func TestUsage_LateEvent(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()
	os := store.NewOrgStore(pool)
	repo := NewTimescaleUsageRepository(pool)

	orgID := createTestOrg(t, os, "late-event-agg")
	createMeter(t, pool, orgID, "late-m", "late-evt", "COUNT", nil, nil)

	now := time.Now().UTC().Truncate(time.Hour)
	lateTime := now.Add(-7 * 24 * time.Hour) // 7 days ago, exact hour boundary

	// Seed 3 events in the current hour.
	for i := 0; i < 3; i++ {
		ts := now.Add(time.Duration(i) * time.Minute)
		ingestEvents(t, pool, orgID, []store.IngestEvent{
			{ID: fmt.Sprintf("now-%d", i), Type: "late-evt", Subject: "s", Time: &ts},
		})
	}
	refreshAggregates(t, pool)

	// Insert 1 late event — 7 days ago.
	ingestEvents(t, pool, orgID, []store.IngestEvent{
		{ID: "late-1", Type: "late-evt", Subject: "s", Time: &lateTime},
	})
	refreshAggregates(t, pool)

	// Query a window that covers both the current hour and the late hour.
	result, err := repo.QueryUsage(ctx, QueryParams{
		OrgID:      orgID,
		MeterSlug:  "late-m",
		MeterType:  "late-evt",
		Agg:        "COUNT",
		From:       lateTime,
		To:         now.Add(time.Hour),
		WindowSize: "HOUR",
	})
	if err != nil {
		t.Fatalf("QueryUsage: %v", err)
	}

	// Find the late bucket and the current bucket.
	buckets := make(map[time.Time]float64)
	for _, dp := range result.Data {
		buckets[dp.Bucket.UTC().Truncate(time.Hour)] = dp.Value
	}

	if got := buckets[lateTime.Truncate(time.Hour)]; got != 1 {
		t.Errorf("late bucket: want 1, got %v", got)
	}
	if got := buckets[now]; got != 3 {
		t.Errorf("current bucket: want 3, got %v", got)
	}
}

// ── FILTERS AND GROUPBY (mandatory) ──────────────────────────────────────────

func TestUsage_SubjectFilter(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()
	os := store.NewOrgStore(pool)
	repo := NewTimescaleUsageRepository(pool)

	orgID := createTestOrg(t, os, "filter-subj")
	createMeter(t, pool, orgID, "filt-m", "filt-evt", "COUNT", nil, nil)

	bucket := time.Date(2026, 2, 10, 8, 0, 0, 0, time.UTC)

	// 3 events for user_a, 2 for user_b.
	for i := 0; i < 3; i++ {
		ts := bucket.Add(time.Duration(i) * time.Minute)
		ingestEvents(t, pool, orgID, []store.IngestEvent{
			{ID: fmt.Sprintf("fa-%d", i), Type: "filt-evt", Subject: "user_a", Time: &ts},
		})
	}
	for i := 0; i < 2; i++ {
		ts := bucket.Add(time.Duration(3+i) * time.Minute)
		ingestEvents(t, pool, orgID, []store.IngestEvent{
			{ID: fmt.Sprintf("fb-%d", i), Type: "filt-evt", Subject: "user_b", Time: &ts},
		})
	}

	result, err := repo.QueryUsage(ctx, QueryParams{
		OrgID:      orgID,
		MeterSlug:  "filt-m",
		MeterType:  "filt-evt",
		Agg:        "COUNT",
		From:       bucket,
		To:         bucket.Add(time.Hour),
		WindowSize: "HOUR",
		Subject:    "user_a",
	})
	if err != nil {
		t.Fatalf("QueryUsage: %v", err)
	}

	total := 0.0
	for _, dp := range result.Data {
		total += dp.Value
	}
	if total != 3 {
		t.Errorf("subject filter: want total=3, got %v", total)
	}
}

func TestUsage_GroupBy(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()
	os := store.NewOrgStore(pool)
	repo := NewTimescaleUsageRepository(pool)

	orgID := createTestOrg(t, os, "groupby-test")
	createMeter(t, pool, orgID, "gb-m", "gb-evt", "COUNT", nil, []string{"model"})

	bucket := time.Date(2026, 3, 5, 6, 0, 0, 0, time.UTC)

	// 3 events with model=gpt-x, 2 with model=gpt-4.
	for i := 0; i < 3; i++ {
		ts := bucket.Add(time.Duration(i) * time.Minute)
		data, _ := json.Marshal(map[string]string{"model": "gpt-x"})
		ingestEvents(t, pool, orgID, []store.IngestEvent{
			{ID: fmt.Sprintf("gx-%d", i), Type: "gb-evt", Subject: "s", Time: &ts, Data: data},
		})
	}
	for i := 0; i < 2; i++ {
		ts := bucket.Add(time.Duration(3+i) * time.Minute)
		data, _ := json.Marshal(map[string]string{"model": "gpt-4"})
		ingestEvents(t, pool, orgID, []store.IngestEvent{
			{ID: fmt.Sprintf("g4-%d", i), Type: "gb-evt", Subject: "s", Time: &ts, Data: data},
		})
	}

	result, err := repo.QueryUsage(ctx, QueryParams{
		OrgID:      orgID,
		MeterSlug:  "gb-m",
		MeterType:  "gb-evt",
		Agg:        "COUNT",
		From:       bucket,
		To:         bucket.Add(time.Hour),
		WindowSize: "HOUR",
		GroupBy:    []string{"model"},
	})
	if err != nil {
		t.Fatalf("QueryUsage: %v", err)
	}

	if len(result.Data) != 2 {
		t.Fatalf("groupBy: want 2 data points (one per model), got %d", len(result.Data))
	}

	byModel := make(map[string]float64)
	for _, dp := range result.Data {
		if dp.Groups == nil {
			t.Error("groups map should not be nil when groupBy is set")
			continue
		}
		model, ok := dp.Groups["model"]
		if !ok {
			t.Error("groups map missing 'model' key")
			continue
		}
		byModel[model] += dp.Value
	}

	if byModel["gpt-x"] != 3 {
		t.Errorf("gpt-x count: want 3, got %v", byModel["gpt-x"])
	}
	if byModel["gpt-4"] != 2 {
		t.Errorf("gpt-4 count: want 2, got %v", byModel["gpt-4"])
	}
}
