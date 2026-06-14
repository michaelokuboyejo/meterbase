package usage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// QueryParams describes a windowed usage query.
type QueryParams struct {
	OrgID      string
	MeterSlug  string   // used only for the response label
	MeterID    string   // UUID; enables per-meter rollup when set
	MeterType  string   // matches events.type
	Agg        string   // SUM|COUNT|AVG|MIN|MAX|UNIQUE_COUNT
	ValueProp  *string  // JSON path in data (nil → use COUNT)
	GroupBy    []string // validated subset of meter.GroupBy
	From       time.Time
	To         time.Time
	WindowSize string // MINUTE|HOUR|DAY|MONTH
	Subject    string // optional filter
	CustomerID string // optional filter
}

// DataPoint is one time-bucket result.
type DataPoint struct {
	Bucket time.Time
	Value  float64
	Groups map[string]string // non-nil only when GroupBy is set
}

// QueryResult is the shaped response.
type QueryResult struct {
	Meter      string
	WindowSize string
	Data       []DataPoint
}

// UsageQuerier abstracts the usage query engine.
type UsageQuerier interface {
	QueryUsage(ctx context.Context, params QueryParams) (*QueryResult, error)
	// TotalUsage returns the single aggregate value over the whole From..To range
	// with no time bucketing. Used by cost computation to get a deterministic total
	// that is independent of continuous-aggregate refresh state.
	TotalUsage(ctx context.Context, params QueryParams) (float64, error)
}

// windowIntervals maps the contract WindowSize values to SQL INTERVAL strings.
var windowIntervals = map[string]string{
	"MINUTE": "1 minute",
	"HOUR":   "1 hour",
	"DAY":    "1 day",
	"MONTH":  "1 month",
}

// TimescaleUsageRepository implements UsageQuerier against PostgreSQL/TimescaleDB.
type TimescaleUsageRepository struct {
	pool *pgxpool.Pool
}

func NewTimescaleUsageRepository(pool *pgxpool.Pool) *TimescaleUsageRepository {
	return &TimescaleUsageRepository{pool: pool}
}

// QueryUsage routes to the best available read path:
//  1. Generic rollup (events_hourly/daily) — unfiltered COUNT on HOUR/DAY
//  2. Per-meter rollup — SUM/AVG/MIN/MAX on HOUR/DAY when a per-meter view exists
//  3. Raw time_bucket aggregation — all other cases
func (r *TimescaleUsageRepository) QueryUsage(ctx context.Context, p QueryParams) (*QueryResult, error) {
	if _, ok := windowIntervals[p.WindowSize]; !ok {
		return nil, fmt.Errorf("invalid windowSize: %s", p.WindowSize)
	}

	var data []DataPoint
	var err error

	switch {
	case canUseRollup(p):
		data, err = r.queryRollup(ctx, p)
	case canUsePerMeterRollup(p):
		data, err = r.queryPerMeterRollup(ctx, p)
	default:
		data, err = r.queryRaw(ctx, p)
	}
	if err != nil {
		return nil, err
	}

	return &QueryResult{
		Meter:      p.MeterSlug,
		WindowSize: p.WindowSize,
		Data:       data,
	}, nil
}

// canUseRollup returns true when the continuous aggregate can serve this query.
// We only use the rollup for unfiltered COUNT queries because:
//   - The rollup's real-time portion only covers recently unmatured data, not
//     historical data that was never materialized. Unfiltered COUNT is the hot
//     path (dashboards showing total event counts); filtered/grouped queries
//     are less frequent and correct on the raw hypertable without this caveat.
//   - GroupBy and customerId filters require per-row data not in the rollup.
func canUseRollup(p QueryParams) bool {
	return p.Agg == "COUNT" &&
		(p.WindowSize == "HOUR" || p.WindowSize == "DAY") &&
		p.Subject == "" &&
		p.CustomerID == "" &&
		len(p.GroupBy) == 0
}

// canUsePerMeterRollup returns true for SUM/AVG/MIN/MAX queries where a per-meter
// continuous aggregate was created for this meter. UNIQUE_COUNT is excluded because
// COUNT(DISTINCT) cannot be correctly summed across subjects in a rollup.
func canUsePerMeterRollup(p QueryParams) bool {
	switch p.Agg {
	case "SUM", "AVG", "MIN", "MAX":
	default:
		return false
	}
	return (p.WindowSize == "HOUR" || p.WindowSize == "DAY") &&
		p.CustomerID == "" &&
		len(p.GroupBy) == 0 &&
		p.MeterID != "" &&
		p.ValueProp != nil &&
		isSafeUUID(p.MeterID)
}

// isSafeUUID is a lightweight check that p.MeterID is a lowercase hex UUID so
// it is safe to embed in the view name string (no SQL-special characters).
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

// queryPerMeterRollup reads SUM/AVG/MIN/MAX from a per-meter continuous aggregate.
// AVG is computed as SUM(sum_value)/SUM(event_count) to correctly re-aggregate
// pre-grouped rows; storing avg() per (subject,bucket) and then re-averaging
// would produce an unweighted mean.
func (r *TimescaleUsageRepository) queryPerMeterRollup(ctx context.Context, p QueryParams) ([]DataPoint, error) {
	suffix := "hourly"
	if p.WindowSize == "DAY" {
		suffix = "daily"
	}
	viewName := "meter_agg_" + strings.ReplaceAll(p.MeterID, "-", "_") + "_" + suffix

	var aggExpr string
	switch p.Agg {
	case "SUM":
		aggExpr = "COALESCE(SUM(sum_value), 0)"
	case "AVG":
		aggExpr = "COALESCE(SUM(sum_value) / NULLIF(SUM(event_count)::numeric, 0), 0)"
	case "MIN":
		aggExpr = "COALESCE(MIN(min_value), 0)"
	case "MAX":
		aggExpr = "COALESCE(MAX(max_value), 0)"
	default:
		return nil, fmt.Errorf("unsupported agg for per-meter rollup: %s", p.Agg)
	}

	var sb strings.Builder
	var args []interface{}
	n := 1

	sb.WriteString(fmt.Sprintf(
		"SELECT bucket, %s::float8 AS value FROM %s WHERE org_id=$1 AND bucket>=$2 AND bucket<$3",
		aggExpr, viewName,
	))
	args = append(args, p.OrgID, p.From, p.To)
	n += 3

	if p.Subject != "" {
		args = append(args, p.Subject)
		sb.WriteString(fmt.Sprintf(" AND subject=$%d", n))
		n++
	}

	sb.WriteString(" GROUP BY bucket ORDER BY bucket")
	_ = n

	return r.scanDataPoints(ctx, sb.String(), args, nil)
}

// queryRollup reads from events_hourly or events_daily.
func (r *TimescaleUsageRepository) queryRollup(ctx context.Context, p QueryParams) ([]DataPoint, error) {
	view := "events_hourly"
	if p.WindowSize == "DAY" {
		view = "events_daily"
	}

	var sb strings.Builder
	var args []interface{}
	n := 1

	sb.WriteString(fmt.Sprintf(
		`SELECT bucket, SUM(event_count)::float8 AS value FROM %s WHERE org_id=$%d AND type=$%d AND bucket>=$%d AND bucket<$%d`,
		view, n, n+1, n+2, n+3,
	))
	args = append(args, p.OrgID, p.MeterType, p.From, p.To)
	n += 4

	if p.Subject != "" {
		args = append(args, p.Subject)
		sb.WriteString(fmt.Sprintf(" AND subject=$%d", n))
		n++
	}

	sb.WriteString(" GROUP BY bucket ORDER BY bucket")

	return r.scanDataPoints(ctx, sb.String(), args, nil)
}

// queryRaw builds a dynamic time_bucket query over the raw events table.
func (r *TimescaleUsageRepository) queryRaw(ctx context.Context, p QueryParams) ([]DataPoint, error) {
	interval, ok := windowIntervals[p.WindowSize]
	if !ok {
		return nil, fmt.Errorf("unknown windowSize: %s", p.WindowSize)
	}

	var sb strings.Builder
	var args []interface{}

	// $1 = interval, $2 = org_id, $3 = type, $4 = from, $5 = to
	args = append(args, interval, p.OrgID, p.MeterType, p.From, p.To)
	n := 6 // next free param index

	// Aggregation expression.
	aggExpr, aggArgs, err := buildAggExpr(p.Agg, p.ValueProp, &n)
	if err != nil {
		return nil, err
	}
	args = append(args, aggArgs...)

	// GroupBy dimension expressions: each becomes a SELECT column + GROUP BY term.
	dimSelects := make([]string, len(p.GroupBy))
	dimGroups := make([]string, len(p.GroupBy))
	for i, dim := range p.GroupBy {
		args = append(args, dim)
		dimSelects[i] = fmt.Sprintf("e.data->>$%d AS grp_%d", n, i)
		dimGroups[i] = fmt.Sprintf("e.data->>$%d", n)
		n++
	}

	sb.WriteString("SELECT time_bucket($1::interval, e.time) AS bucket, ")
	sb.WriteString(aggExpr)
	sb.WriteString(" AS value")
	for _, ds := range dimSelects {
		sb.WriteString(", ")
		sb.WriteString(ds)
	}

	sb.WriteString(" FROM events e WHERE e.org_id=$2 AND e.type=$3 AND e.time>=$4 AND e.time<$5")

	if p.Subject != "" {
		args = append(args, p.Subject)
		sb.WriteString(fmt.Sprintf(" AND e.subject=$%d", n))
		n++
	}
	if p.CustomerID != "" {
		args = append(args, p.CustomerID)
		sb.WriteString(fmt.Sprintf(" AND e.customer_id=$%d::uuid", n))
		n++
	}

	sb.WriteString(" GROUP BY 1")
	for _, dg := range dimGroups {
		sb.WriteString(", ")
		sb.WriteString(dg)
	}
	sb.WriteString(" ORDER BY 1")

	return r.scanDataPoints(ctx, sb.String(), args, p.GroupBy)
}

// buildAggExpr returns the SQL aggregation expression for the given agg+valueProp.
// It appends any needed query arguments to args via nextN (the running param counter).
func buildAggExpr(agg string, valueProp *string, nextN *int) (string, []interface{}, error) {
	var args []interface{}
	var expr string

	switch agg {
	case "COUNT":
		expr = "COUNT(*)"
	case "SUM":
		if valueProp == nil {
			return "", nil, fmt.Errorf("SUM requires valueProperty")
		}
		args = append(args, *valueProp)
		expr = fmt.Sprintf("COALESCE(SUM((e.data->>$%d)::numeric), 0)", *nextN)
		*nextN++
	case "AVG":
		if valueProp == nil {
			return "", nil, fmt.Errorf("AVG requires valueProperty")
		}
		args = append(args, *valueProp)
		expr = fmt.Sprintf("COALESCE(AVG((e.data->>$%d)::numeric), 0)", *nextN)
		*nextN++
	case "MIN":
		if valueProp == nil {
			return "", nil, fmt.Errorf("MIN requires valueProperty")
		}
		args = append(args, *valueProp)
		expr = fmt.Sprintf("COALESCE(MIN((e.data->>$%d)::numeric), 0)", *nextN)
		*nextN++
	case "MAX":
		if valueProp == nil {
			return "", nil, fmt.Errorf("MAX requires valueProperty")
		}
		args = append(args, *valueProp)
		expr = fmt.Sprintf("COALESCE(MAX((e.data->>$%d)::numeric), 0)", *nextN)
		*nextN++
	case "UNIQUE_COUNT":
		if valueProp == nil {
			return "", nil, fmt.Errorf("UNIQUE_COUNT requires valueProperty")
		}
		args = append(args, *valueProp)
		expr = fmt.Sprintf("COUNT(DISTINCT e.data->>$%d)", *nextN)
		*nextN++
	default:
		return "", nil, fmt.Errorf("unknown aggregation: %s", agg)
	}
	return expr, args, nil
}

// scanDataPoints executes q with args and scans the result into DataPoint slice.
// groupByDims is the ordered list of groupBy dimension names (for Groups map keys).
func (r *TimescaleUsageRepository) scanDataPoints(ctx context.Context, q string, args []interface{}, groupByDims []string) ([]DataPoint, error) {
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query usage: %w", err)
	}
	defer rows.Close()

	var data []DataPoint
	for rows.Next() {
		var bucket time.Time
		var value float64

		// Build scan destinations: bucket, value, then one string per groupBy dim.
		dimVals := make([]*string, len(groupByDims))
		scanDests := make([]interface{}, 2+len(groupByDims))
		scanDests[0] = &bucket
		scanDests[1] = &value
		for i := range dimVals {
			dimVals[i] = new(string)
			scanDests[2+i] = dimVals[i]
		}

		if err := rows.Scan(scanDests...); err != nil {
			return nil, fmt.Errorf("scan data point: %w", err)
		}

		dp := DataPoint{Bucket: bucket.UTC(), Value: value}
		if len(groupByDims) > 0 {
			dp.Groups = make(map[string]string, len(groupByDims))
			for i, dim := range groupByDims {
				if dimVals[i] != nil {
					dp.Groups[dim] = *dimVals[i]
				}
			}
		}
		data = append(data, dp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage rows: %w", err)
	}

	if data == nil {
		data = []DataPoint{}
	}
	return data, nil
}

// TotalUsage returns the single aggregate value over the full From..To range with
// no time bucketing. It always reads raw events — never rollups — so the result is
// deterministic regardless of whether continuous aggregates have refreshed.
func (r *TimescaleUsageRepository) TotalUsage(ctx context.Context, p QueryParams) (float64, error) {
	n := 5 // next free param index after org_id, type, from, to
	aggExpr, aggArgs, err := buildAggExpr(p.Agg, p.ValueProp, &n)
	if err != nil {
		return 0, err
	}

	var sb strings.Builder
	args := []interface{}{p.OrgID, p.MeterType, p.From, p.To}
	args = append(args, aggArgs...)

	sb.WriteString("SELECT COALESCE(")
	sb.WriteString(aggExpr)
	sb.WriteString(", 0)::float8 AS value FROM events e WHERE e.org_id=$1 AND e.type=$2 AND e.time>=$3 AND e.time<$4")

	if p.Subject != "" {
		args = append(args, p.Subject)
		sb.WriteString(fmt.Sprintf(" AND e.subject=$%d", n))
		n++
	}
	if p.CustomerID != "" {
		args = append(args, p.CustomerID)
		sb.WriteString(fmt.Sprintf(" AND e.customer_id=$%d::uuid", n))
		n++
	}
	_ = n

	var value float64
	if err := r.pool.QueryRow(ctx, sb.String(), args...).Scan(&value); err != nil {
		return 0, fmt.Errorf("total usage: %w", err)
	}
	return value, nil
}
