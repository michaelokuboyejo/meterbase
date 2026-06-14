package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// StoredEvent is the full event record returned by listing/export.
type StoredEvent struct {
	ID         string
	Type       string
	Source     string
	Subject    string
	CustomerID *string
	Time       time.Time
	IngestedAt time.Time
	Data       json.RawMessage
}

// ListEventsParams holds filter/pagination options for listing events.
type ListEventsParams struct {
	From    *time.Time
	To      *time.Time
	Subject string
	Type    string
	Limit   int    // 0 → default 100
	Cursor  string // opaque; encodes (time, id)
}

// IngestEvent is the input shape for a single event to be persisted.
type IngestEvent struct {
	ID      string
	Type    string
	Source  string
	Subject string
	Time    *time.Time      // nil → server uses now()
	Data    json.RawMessage // nil/empty → stored as '{}'
}

// TimescaleEventStore persists events into the TimescaleDB hypertable.
type TimescaleEventStore struct {
	pool *pgxpool.Pool
}

func NewTimescaleEventStore(pool *pgxpool.Pool) *TimescaleEventStore {
	return &TimescaleEventStore{pool: pool}
}

// IngestEvents inserts a batch of events, deduplicating on (org_id, id).
// Dedup is a pre-check SELECT; ON CONFLICT DO NOTHING is a safety net for races.
// Same-batch duplicates are also detected via an in-memory seen set.
// Events are stored regardless of whether a matching meter exists (store-and-flag).
//
// Returns received (new rows inserted) and duplicates (skipped).
func (s *TimescaleEventStore) IngestEvents(ctx context.Context, orgID string, events []IngestEvent) (received, duplicates int, err error) {
	if len(events) == 0 {
		return 0, 0, nil
	}

	// Collect all incoming IDs for the pre-check query.
	ids := make([]string, len(events))
	for i, e := range events {
		ids[i] = e.ID
	}

	// Pre-check: which IDs already exist for this org?
	rows, qErr := s.pool.Query(ctx,
		`SELECT DISTINCT id FROM events WHERE org_id = $1 AND id = ANY($2::text[])`,
		orgID, ids,
	)
	if qErr != nil {
		return 0, 0, fmt.Errorf("dedup check: %w", qErr)
	}
	existingIDs := make(map[string]struct{})
	for rows.Next() {
		var id string
		if scanErr := rows.Scan(&id); scanErr != nil {
			rows.Close()
			return 0, 0, fmt.Errorf("scan dedup row: %w", scanErr)
		}
		existingIDs[id] = struct{}{}
	}
	rows.Close()
	if rowsErr := rows.Err(); rowsErr != nil {
		return 0, 0, fmt.Errorf("dedup rows: %w", rowsErr)
	}

	// Insert non-duplicates; seenInBatch handles same-id within one request.
	seenInBatch := make(map[string]struct{})
	for _, e := range events {
		if _, exists := existingIDs[e.ID]; exists {
			duplicates++
			continue
		}
		if _, seen := seenInBatch[e.ID]; seen {
			duplicates++
			continue
		}

		t := time.Now()
		if e.Time != nil {
			t = *e.Time
		}
		data := e.Data
		if len(data) == 0 {
			data = json.RawMessage("{}")
		}

		tag, insertErr := s.pool.Exec(ctx,
			`INSERT INTO events (org_id, id, type, source, subject, customer_id, time, data)
			 VALUES ($1, $2, $3, NULLIF($4, ''), $5, NULL, $6, $7)
			 ON CONFLICT (org_id, id, time) DO NOTHING`,
			orgID, e.ID, e.Type, e.Source, e.Subject, t, []byte(data),
		)
		if insertErr != nil {
			return received, duplicates, fmt.Errorf("insert event %s: %w", e.ID, insertErr)
		}
		if tag.RowsAffected() == 0 {
			// Race: another goroutine inserted same (org, id, time) between SELECT and INSERT.
			duplicates++
		} else {
			received++
			seenInBatch[e.ID] = struct{}{}
		}
	}

	return received, duplicates, nil
}

// ListEvents returns a paginated page of raw events for an org.
// Keyset cursor encodes (time DESC, id DESC) so the sort is stable.
func (s *TimescaleEventStore) ListEvents(ctx context.Context, orgID string, p ListEventsParams) ([]StoredEvent, string, error) {
	limit := p.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	var sb strings.Builder
	var args []interface{}
	n := 1

	sb.WriteString(`SELECT id, type, COALESCE(source,''), subject, customer_id, time, ingested_at, data FROM events WHERE org_id=$1`)
	args = append(args, orgID)
	n++

	if p.From != nil {
		args = append(args, *p.From)
		sb.WriteString(fmt.Sprintf(" AND time>=$%d", n))
		n++
	}
	if p.To != nil {
		args = append(args, *p.To)
		sb.WriteString(fmt.Sprintf(" AND time<$%d", n))
		n++
	}
	if p.Subject != "" {
		args = append(args, p.Subject)
		sb.WriteString(fmt.Sprintf(" AND subject=$%d", n))
		n++
	}
	if p.Type != "" {
		args = append(args, p.Type)
		sb.WriteString(fmt.Sprintf(" AND type=$%d", n))
		n++
	}

	// Cursor: decode (cursorTime, cursorID) and apply keyset condition.
	if p.Cursor != "" {
		ct, cid, err := decodeCursor(p.Cursor)
		if err == nil {
			args = append(args, ct, cid)
			sb.WriteString(fmt.Sprintf(" AND (time < $%d OR (time = $%d AND id > $%d))", n, n, n+1))
			n += 2
		}
	}

	sb.WriteString(fmt.Sprintf(" ORDER BY time DESC, id ASC LIMIT $%d", n))
	args = append(args, limit+1)

	rows, err := s.pool.Query(ctx, sb.String(), args...)
	if err != nil {
		return nil, "", fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	var events []StoredEvent
	for rows.Next() {
		var e StoredEvent
		if err := rows.Scan(&e.ID, &e.Type, &e.Source, &e.Subject, &e.CustomerID, &e.Time, &e.IngestedAt, &e.Data); err != nil {
			return nil, "", fmt.Errorf("scan event: %w", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("event rows: %w", err)
	}

	var nextCursor string
	if len(events) > limit {
		last := events[limit-1]
		nextCursor = encodeCursor(last.Time, last.ID)
		events = events[:limit]
	}
	if events == nil {
		events = []StoredEvent{}
	}
	return events, nextCursor, nil
}

// ExportEvents returns all matching events for an org (no pagination cap).
func (s *TimescaleEventStore) ExportEvents(ctx context.Context, orgID string, p ListEventsParams) ([]StoredEvent, error) {
	var sb strings.Builder
	var args []interface{}
	n := 1

	sb.WriteString(`SELECT id, type, COALESCE(source,''), subject, customer_id, time, ingested_at, data FROM events WHERE org_id=$1`)
	args = append(args, orgID)
	n++

	if p.From != nil {
		args = append(args, *p.From)
		sb.WriteString(fmt.Sprintf(" AND time>=$%d", n))
		n++
	}
	if p.To != nil {
		args = append(args, *p.To)
		sb.WriteString(fmt.Sprintf(" AND time<$%d", n))
		n++
	}
	if p.Subject != "" {
		args = append(args, p.Subject)
		sb.WriteString(fmt.Sprintf(" AND subject=$%d", n))
		n++
	}
	if p.Type != "" {
		args = append(args, p.Type)
		sb.WriteString(fmt.Sprintf(" AND type=$%d", n))
		n++
	}

	_ = n
	sb.WriteString(" ORDER BY time DESC, id ASC")

	rows, err := s.pool.Query(ctx, sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("export events: %w", err)
	}
	defer rows.Close()

	var events []StoredEvent
	for rows.Next() {
		var e StoredEvent
		if err := rows.Scan(&e.ID, &e.Type, &e.Source, &e.Subject, &e.CustomerID, &e.Time, &e.IngestedAt, &e.Data); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("export rows: %w", err)
	}

	if events == nil {
		events = []StoredEvent{}
	}
	return events, nil
}
