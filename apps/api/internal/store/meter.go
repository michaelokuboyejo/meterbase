package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// sqlEscapeString makes a string safe for embedding in DDL as a SQL literal
// (doubles single-quotes). Only used where parameterised queries aren't
// possible (continuous-aggregate DDL cannot run in a transaction).
func sqlEscapeString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// isSafeUUID checks that s is a lowercase hex UUID — safe to embed in a
// view name without further quoting.
func isSafeUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || c == '-') {
			return false
		}
	}
	return true
}

// MeterViewName returns the continuous-aggregate view name for a meter + window.
// Exported so the usage package can derive the same name without importing meter store.
func MeterViewName(meterID, window string) string {
	return "meter_agg_" + strings.ReplaceAll(meterID, "-", "_") + "_" + window
}

var ErrMeterNotFound = errors.New("meter not found")
var ErrDuplicateMeterSlug = errors.New("meter slug already exists for this org")

// Meter is a measurement definition belonging to an org.
type Meter struct {
	ID            string
	OrgID         string
	Slug          string
	EventType     string
	Aggregation   string
	ValueProperty *string
	GroupBy       []string
	CreatedAt     time.Time
}

// MeterStore handles meter persistence.
type MeterStore struct {
	pool *pgxpool.Pool
}

func NewMeterStore(pool *pgxpool.Pool) *MeterStore {
	return &MeterStore{pool: pool}
}

// createMeterAggregates creates per-meter continuous aggregates for HOUR and DAY
// windows. Only useful for meters with a valueProp (SUM/AVG/MIN/MAX).
// CREATE MATERIALIZED VIEW ... WITH (timescaledb.continuous) cannot run inside a
// transaction, so this uses pool.Exec directly.
func (s *MeterStore) createMeterAggregates(ctx context.Context, meterID, eventType, valueProp string) error {
	if !isSafeUUID(meterID) {
		return fmt.Errorf("invalid meter id")
	}
	safeType := sqlEscapeString(eventType)
	safeProp := sqlEscapeString(valueProp)

	type win struct {
		name     string
		interval string
		startOff string
		endOff   string
	}
	for _, w := range []win{
		{"hourly", "1 hour", "30 days", "1 hour"},
		{"daily", "1 day", "90 days", "1 day"},
	} {
		viewName := MeterViewName(meterID, w.name)
		createSQL := fmt.Sprintf(`
CREATE MATERIALIZED VIEW IF NOT EXISTS %s
WITH (timescaledb.continuous, timescaledb.materialized_only = false) AS
SELECT
  org_id,
  subject,
  time_bucket('%s', time) AS bucket,
  count(*) AS event_count,
  sum((data->>'%s')::numeric) AS sum_value,
  min((data->>'%s')::numeric) AS min_value,
  max((data->>'%s')::numeric) AS max_value
FROM events
WHERE type = '%s'
GROUP BY org_id, subject, bucket`,
			viewName, w.interval,
			safeProp, safeProp, safeProp,
			safeType,
		)
		if _, err := s.pool.Exec(ctx, createSQL); err != nil {
			return fmt.Errorf("create %s: %w", viewName, err)
		}
		policySQL := fmt.Sprintf(`
SELECT add_continuous_aggregate_policy('%s',
  start_offset      => INTERVAL '%s',
  end_offset        => INTERVAL '%s',
  schedule_interval => INTERVAL '1 minute')`,
			viewName, w.startOff, w.endOff,
		)
		// Ignore "policy already exists" — idempotent on re-run.
		_, _ = s.pool.Exec(ctx, policySQL)
	}
	return nil
}

// dropMeterAggregates removes the per-meter continuous aggregates.
func (s *MeterStore) dropMeterAggregates(ctx context.Context, meterID string) error {
	for _, window := range []string{"daily", "hourly"} {
		viewName := MeterViewName(meterID, window)
		if _, err := s.pool.Exec(ctx,
			fmt.Sprintf("DROP MATERIALIZED VIEW IF EXISTS %s CASCADE", viewName),
		); err != nil {
			return fmt.Errorf("drop %s: %w", viewName, err)
		}
	}
	return nil
}

// CreateMeter inserts a new meter scoped to orgID.
// Returns ErrDuplicateMeterSlug on (org_id, slug) conflict.
// When valueProp is non-nil, per-meter continuous aggregates are also created.
func (s *MeterStore) CreateMeter(ctx context.Context, orgID, slug, eventType, agg string, valueProp *string, groupBy []string) (*Meter, error) {
	if groupBy == nil {
		groupBy = []string{}
	}
	var m Meter
	var groupByArr []string
	err := s.pool.QueryRow(ctx,
		`INSERT INTO meters (org_id, slug, event_type, agg, value_prop, group_by)
		 VALUES ($1, $2, $3, $4::aggregation, $5, $6)
		 RETURNING id, org_id, slug, event_type, agg::text, value_prop, group_by, created_at`,
		orgID, slug, eventType, agg, valueProp, groupBy,
	).Scan(&m.ID, &m.OrgID, &m.Slug, &m.EventType, &m.Aggregation, &m.ValueProperty, &groupByArr, &m.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDuplicateMeterSlug
		}
		return nil, fmt.Errorf("create meter: %w", err)
	}
	m.GroupBy = coalesceStrSlice(groupByArr)

	if valueProp != nil {
		if aggErr := s.createMeterAggregates(ctx, m.ID, m.EventType, *valueProp); aggErr != nil {
			// Aggregate creation failed: clean up the meter row to keep state consistent.
			_, _ = s.pool.Exec(ctx, `DELETE FROM meters WHERE id = $1`, m.ID)
			return nil, fmt.Errorf("create meter aggregates: %w", aggErr)
		}
	}
	return &m, nil
}

// GetMeterBySlug fetches a meter by slug, scoped to orgID.
func (s *MeterStore) GetMeterBySlug(ctx context.Context, orgID, slug string) (*Meter, error) {
	var m Meter
	var groupByArr []string
	err := s.pool.QueryRow(ctx,
		`SELECT id, org_id, slug, event_type, agg::text, value_prop, group_by, created_at
		 FROM meters WHERE org_id = $1 AND slug = $2`,
		orgID, slug,
	).Scan(&m.ID, &m.OrgID, &m.Slug, &m.EventType, &m.Aggregation, &m.ValueProperty, &groupByArr, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMeterNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get meter: %w", err)
	}
	m.GroupBy = coalesceStrSlice(groupByArr)
	return &m, nil
}

// ListMeters returns a page of meters for orgID ordered by (created_at ASC, id ASC).
func (s *MeterStore) ListMeters(ctx context.Context, orgID string, limit int, cursor string) ([]*Meter, string, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	var (
		rows pgx.Rows
		err  error
	)
	if cursor == "" {
		rows, err = s.pool.Query(ctx,
			`SELECT id, org_id, slug, event_type, agg::text, value_prop, group_by, created_at
			 FROM meters WHERE org_id = $1
			 ORDER BY created_at ASC, id ASC
			 LIMIT $2`,
			orgID, limit+1,
		)
	} else {
		ct, cid, decErr := decodeCursor(cursor)
		if decErr != nil {
			return nil, "", fmt.Errorf("invalid cursor: %w", decErr)
		}
		rows, err = s.pool.Query(ctx,
			`SELECT id, org_id, slug, event_type, agg::text, value_prop, group_by, created_at
			 FROM meters
			 WHERE org_id = $1 AND (
			   created_at > $2 OR (created_at = $2 AND id > $3::uuid)
			 )
			 ORDER BY created_at ASC, id ASC
			 LIMIT $4`,
			orgID, ct, cid, limit+1,
		)
	}
	if err != nil {
		return nil, "", fmt.Errorf("list meters: %w", err)
	}
	defer rows.Close()

	var meters []*Meter
	for rows.Next() {
		var m Meter
		var groupByArr []string
		if err := rows.Scan(&m.ID, &m.OrgID, &m.Slug, &m.EventType, &m.Aggregation, &m.ValueProperty, &groupByArr, &m.CreatedAt); err != nil {
			return nil, "", fmt.Errorf("scan meter: %w", err)
		}
		m.GroupBy = coalesceStrSlice(groupByArr)
		meters = append(meters, &m)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("list meters: %w", err)
	}

	var nextCursor string
	if len(meters) > limit {
		meters = meters[:limit]
		last := meters[len(meters)-1]
		nextCursor = encodeCursor(last.CreatedAt, last.ID)
	}
	return meters, nextCursor, nil
}

// GetMeterByID fetches a meter by UUID, scoped to orgID.
func (s *MeterStore) GetMeterByID(ctx context.Context, orgID, meterID string) (*Meter, error) {
	var m Meter
	var groupByArr []string
	err := s.pool.QueryRow(ctx,
		`SELECT id, org_id, slug, event_type, agg::text, value_prop, group_by, created_at
		 FROM meters WHERE org_id = $1 AND id = $2::uuid`,
		orgID, meterID,
	).Scan(&m.ID, &m.OrgID, &m.Slug, &m.EventType, &m.Aggregation, &m.ValueProperty, &groupByArr, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMeterNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get meter by id: %w", err)
	}
	m.GroupBy = coalesceStrSlice(groupByArr)
	return &m, nil
}

// DeleteMeter removes a meter by slug, scoped to orgID.
// Per-meter continuous aggregates (if any) are dropped first.
func (s *MeterStore) DeleteMeter(ctx context.Context, orgID, slug string) error {
	// Fetch ID and whether this meter has per-meter aggregates.
	var meterID string
	var hasValueProp bool
	err := s.pool.QueryRow(ctx,
		`SELECT id, value_prop IS NOT NULL FROM meters WHERE org_id = $1 AND slug = $2`,
		orgID, slug,
	).Scan(&meterID, &hasValueProp)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrMeterNotFound
	}
	if err != nil {
		return fmt.Errorf("lookup meter for delete: %w", err)
	}

	if hasValueProp {
		if err := s.dropMeterAggregates(ctx, meterID); err != nil {
			return fmt.Errorf("drop meter aggregates: %w", err)
		}
	}

	tag, err := s.pool.Exec(ctx,
		`DELETE FROM meters WHERE org_id = $1 AND id = $2`,
		orgID, meterID,
	)
	if err != nil {
		return fmt.Errorf("delete meter: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrMeterNotFound
	}
	return nil
}

// coalesceStrSlice returns an empty (non-nil) slice when arr is nil.
func coalesceStrSlice(arr []string) []string {
	if arr == nil {
		return []string{}
	}
	return arr
}
