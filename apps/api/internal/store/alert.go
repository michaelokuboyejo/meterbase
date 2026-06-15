package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrAlertRuleNotFound = errors.New("alert rule not found")
var ErrWebhookEndpointNotFound = errors.New("webhook endpoint not found")

// AlertRuleInput holds fields for creating an alert rule.
type AlertRuleInput struct {
	MeterID   string
	Scope     string
	Window    string
	Threshold float64
	Enabled   bool
}

// AlertRule is the persisted alert rule.
type AlertRule struct {
	ID        string
	OrgID     string
	MeterID   string
	Scope     string
	Window    string
	Threshold float64
	Enabled   bool
	CreatedAt time.Time
}

// AlertRuleWithMeter is an alert rule joined with its meter's metadata.
// Used by the evaluator to avoid a second lookup per rule.
type AlertRuleWithMeter struct {
	AlertRule
	MeterSlug      string
	MeterEventType string
	MeterAgg       string
	MeterValueProp *string
}

// WebhookEndpoint is a registered delivery target.
type WebhookEndpoint struct {
	ID      string
	OrgID   string
	URL     string
	Secret  string // raw HMAC key returned once on creation
	Enabled bool
}

// WebhookDelivery is a single delivery attempt record.
type WebhookDelivery struct {
	ID          string
	EndpointID  string
	EventType   string
	Payload     json.RawMessage
	Status      string
	Attempts    int
	LastAttempt *time.Time
	CreatedAt   time.Time
}

// WebhookDeliveryWithEndpoint is a delivery joined with its endpoint's delivery details.
// Used by the dispatcher to avoid a second lookup per delivery.
type WebhookDeliveryWithEndpoint struct {
	WebhookDelivery
	URL    string
	Secret string
}

// AlertStore persists alert rules, firings, webhook endpoints, and deliveries.
type AlertStore struct {
	pool *pgxpool.Pool
}

func NewAlertStore(pool *pgxpool.Pool) *AlertStore {
	return &AlertStore{pool: pool}
}

// CreateAlertRule inserts a new alert rule scoped to orgID.
func (s *AlertStore) CreateAlertRule(ctx context.Context, orgID string, in AlertRuleInput) (*AlertRule, error) {
	var r AlertRule
	err := s.pool.QueryRow(ctx,
		`INSERT INTO alert_rules (org_id, meter_id, scope, alert_window, threshold, enabled)
		 VALUES ($1, $2::uuid, $3, $4, $5, $6)
		 RETURNING id, org_id, meter_id, scope, alert_window, threshold, enabled, created_at`,
		orgID, in.MeterID, in.Scope, in.Window, in.Threshold, in.Enabled,
	).Scan(&r.ID, &r.OrgID, &r.MeterID, &r.Scope, &r.Window, &r.Threshold, &r.Enabled, &r.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create alert rule: %w", err)
	}
	return &r, nil
}

// ListAlertRules returns a page of alert rules for orgID, ordered by (created_at ASC, id ASC).
func (s *AlertStore) ListAlertRules(ctx context.Context, orgID string, limit int, cursor string) ([]*AlertRule, string, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	var rows pgx.Rows
	var err error
	if cursor == "" {
		rows, err = s.pool.Query(ctx,
			`SELECT id, org_id, meter_id, scope, alert_window, threshold, enabled, created_at
			 FROM alert_rules WHERE org_id = $1
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
			`SELECT id, org_id, meter_id, scope, alert_window, threshold, enabled, created_at
			 FROM alert_rules
			 WHERE org_id = $1 AND (
			   created_at > $2 OR (created_at = $2 AND id > $3::uuid)
			 )
			 ORDER BY created_at ASC, id ASC
			 LIMIT $4`,
			orgID, ct, cid, limit+1,
		)
	}
	if err != nil {
		return nil, "", fmt.Errorf("list alert rules: %w", err)
	}
	defer rows.Close()

	var rules []*AlertRule
	for rows.Next() {
		var r AlertRule
		if err := rows.Scan(&r.ID, &r.OrgID, &r.MeterID, &r.Scope, &r.Window, &r.Threshold, &r.Enabled, &r.CreatedAt); err != nil {
			return nil, "", fmt.Errorf("scan alert rule: %w", err)
		}
		rules = append(rules, &r)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("list alert rules rows: %w", err)
	}

	var nextCursor string
	if len(rules) > limit {
		rules = rules[:limit]
		last := rules[len(rules)-1]
		nextCursor = encodeCursor(last.CreatedAt, last.ID)
	}
	return rules, nextCursor, nil
}

// LoadEnabledRules returns all enabled alert rules joined with their meter metadata.
// Called by the evaluator on each tick; not scoped to a single org.
func (s *AlertStore) LoadEnabledRules(ctx context.Context) ([]*AlertRuleWithMeter, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT ar.id, ar.org_id, ar.meter_id, ar.scope, ar.alert_window, ar.threshold, ar.enabled, ar.created_at,
		        m.slug, m.event_type, m.agg::text, m.value_prop
		 FROM alert_rules ar
		 JOIN meters m ON m.id = ar.meter_id
		 WHERE ar.enabled = true`,
	)
	if err != nil {
		return nil, fmt.Errorf("load enabled rules: %w", err)
	}
	defer rows.Close()

	var rules []*AlertRuleWithMeter
	for rows.Next() {
		var r AlertRuleWithMeter
		if err := rows.Scan(
			&r.ID, &r.OrgID, &r.MeterID, &r.Scope, &r.Window, &r.Threshold, &r.Enabled, &r.CreatedAt,
			&r.MeterSlug, &r.MeterEventType, &r.MeterAgg, &r.MeterValueProp,
		); err != nil {
			return nil, fmt.Errorf("scan alert rule with meter: %w", err)
		}
		rules = append(rules, &r)
	}
	return rules, rows.Err()
}

// GetDistinctScopeKeys returns the distinct scope key values present in the events
// table for the given org, event type, and time range.
// For scopeType "subject" it returns subject values; for "customer" it returns
// customer UUIDs (as strings). Global scope is handled by the caller.
func (s *AlertStore) GetDistinctScopeKeys(ctx context.Context, orgID, eventType, scopeType string, from, to time.Time) ([]string, error) {
	var q string
	if scopeType == "subject" {
		q = `SELECT DISTINCT subject FROM events
		     WHERE org_id=$1 AND type=$2 AND time>=$3 AND time<$4`
	} else {
		q = `SELECT DISTINCT customer_id::text FROM events
		     WHERE org_id=$1 AND type=$2 AND time>=$3 AND time<$4 AND customer_id IS NOT NULL`
	}

	rows, err := s.pool.Query(ctx, q, orgID, eventType, from, to)
	if err != nil {
		return nil, fmt.Errorf("get distinct scope keys: %w", err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, fmt.Errorf("scan scope key: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// TryRecordFiring atomically records a first crossing for (ruleID, scopeKey, windowStart).
// Returns true if this call was the first crossing (delivery should be created);
// returns false if a prior crossing already exists (de-bounce: skip delivery).
func (s *AlertStore) TryRecordFiring(ctx context.Context, ruleID, scopeKey string, windowStart time.Time) (bool, error) {
	var id string
	err := s.pool.QueryRow(ctx,
		`INSERT INTO alert_firings (rule_id, scope_key, window_start)
		 VALUES ($1::uuid, $2, $3)
		 ON CONFLICT (rule_id, scope_key, window_start) DO NOTHING
		 RETURNING id`,
		ruleID, scopeKey, windowStart,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // conflict — already fired in this window period
	}
	if err != nil {
		return false, fmt.Errorf("try record firing: %w", err)
	}
	return true, nil
}

// CreateWebhookEndpoint registers a new webhook endpoint for orgID.
// The caller should generate a random secret and pass it as the secret parameter;
// the raw secret is stored and returned once to the user.
func (s *AlertStore) CreateWebhookEndpoint(ctx context.Context, orgID, url, secret string) (*WebhookEndpoint, error) {
	var ep WebhookEndpoint
	err := s.pool.QueryRow(ctx,
		`INSERT INTO webhook_endpoints (org_id, url, secret, enabled)
		 VALUES ($1, $2, $3, true)
		 RETURNING id, org_id, url, secret, enabled`,
		orgID, url, secret,
	).Scan(&ep.ID, &ep.OrgID, &ep.URL, &ep.Secret, &ep.Enabled)
	if err != nil {
		return nil, fmt.Errorf("create webhook endpoint: %w", err)
	}
	return &ep, nil
}

// GetWebhookEndpoint fetches a single endpoint by ID, scoped to orgID.
func (s *AlertStore) GetWebhookEndpoint(ctx context.Context, orgID, endpointID string) (*WebhookEndpoint, error) {
	var ep WebhookEndpoint
	err := s.pool.QueryRow(ctx,
		`SELECT id, org_id, url, secret, enabled FROM webhook_endpoints WHERE org_id=$1 AND id=$2::uuid`,
		orgID, endpointID,
	).Scan(&ep.ID, &ep.OrgID, &ep.URL, &ep.Secret, &ep.Enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrWebhookEndpointNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get webhook endpoint: %w", err)
	}
	return &ep, nil
}

// ListEnabledEndpointsByOrg returns all enabled endpoints for orgID.
// Used by the evaluator to fan out deliveries after a threshold crossing.
func (s *AlertStore) ListEnabledEndpointsByOrg(ctx context.Context, orgID string) ([]*WebhookEndpoint, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, org_id, url, secret, enabled FROM webhook_endpoints WHERE org_id=$1 AND enabled=true`,
		orgID,
	)
	if err != nil {
		return nil, fmt.Errorf("list enabled endpoints: %w", err)
	}
	defer rows.Close()

	var eps []*WebhookEndpoint
	for rows.Next() {
		var ep WebhookEndpoint
		if err := rows.Scan(&ep.ID, &ep.OrgID, &ep.URL, &ep.Secret, &ep.Enabled); err != nil {
			return nil, fmt.Errorf("scan endpoint: %w", err)
		}
		eps = append(eps, &ep)
	}
	return eps, rows.Err()
}

// ListWebhookEndpoints returns a paginated list of all endpoints for orgID (enabled and disabled).
func (s *AlertStore) ListWebhookEndpoints(ctx context.Context, orgID string, limit int, cursor string) ([]*WebhookEndpoint, string, error) {
	var rows pgx.Rows
	var err error
	if cursor == "" {
		rows, err = s.pool.Query(ctx,
			`SELECT id, org_id, url, secret, enabled FROM webhook_endpoints
			 WHERE org_id=$1 ORDER BY id LIMIT $2`,
			orgID, limit+1,
		)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT id, org_id, url, secret, enabled FROM webhook_endpoints
			 WHERE org_id=$1 AND id > $2::uuid ORDER BY id LIMIT $3`,
			orgID, cursor, limit+1,
		)
	}
	if err != nil {
		return nil, "", fmt.Errorf("list webhook endpoints: %w", err)
	}
	defer rows.Close()

	var eps []*WebhookEndpoint
	for rows.Next() {
		var ep WebhookEndpoint
		if err := rows.Scan(&ep.ID, &ep.OrgID, &ep.URL, &ep.Secret, &ep.Enabled); err != nil {
			return nil, "", fmt.Errorf("scan endpoint: %w", err)
		}
		eps = append(eps, &ep)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	nextCursor := ""
	if len(eps) > limit {
		nextCursor = eps[limit].ID
		eps = eps[:limit]
	}
	return eps, nextCursor, nil
}

// CreateDelivery enqueues a delivery for the given endpoint.
func (s *AlertStore) CreateDelivery(ctx context.Context, endpointID, eventType string, payload json.RawMessage) (*WebhookDelivery, error) {
	var d WebhookDelivery
	var payBytes []byte
	err := s.pool.QueryRow(ctx,
		`INSERT INTO webhook_deliveries (endpoint_id, event_type, payload)
		 VALUES ($1::uuid, $2, $3)
		 RETURNING id, endpoint_id, event_type, payload, status, attempts, last_attempt, created_at`,
		endpointID, eventType, []byte(payload),
	).Scan(&d.ID, &d.EndpointID, &d.EventType, &payBytes, &d.Status, &d.Attempts, &d.LastAttempt, &d.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create delivery: %w", err)
	}
	d.Payload = json.RawMessage(payBytes)
	return &d, nil
}

// ClaimPendingDeliveries returns up to limit pending deliveries whose next_attempt
// is due (NULL or in the past), joined with the endpoint URL and secret.
func (s *AlertStore) ClaimPendingDeliveries(ctx context.Context, limit int) ([]*WebhookDeliveryWithEndpoint, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT d.id, d.endpoint_id, d.event_type, d.payload, d.status, d.attempts, d.last_attempt, d.created_at,
		        e.url, e.secret
		 FROM webhook_deliveries d
		 JOIN webhook_endpoints e ON e.id = d.endpoint_id
		 WHERE d.status = 'pending'
		   AND (d.next_attempt IS NULL OR d.next_attempt <= now())
		   AND e.enabled = true
		 ORDER BY d.created_at ASC
		 LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("claim pending deliveries: %w", err)
	}
	defer rows.Close()

	var dels []*WebhookDeliveryWithEndpoint
	for rows.Next() {
		var d WebhookDeliveryWithEndpoint
		var payBytes []byte
		if err := rows.Scan(
			&d.ID, &d.EndpointID, &d.EventType, &payBytes, &d.Status, &d.Attempts, &d.LastAttempt, &d.CreatedAt,
			&d.URL, &d.Secret,
		); err != nil {
			return nil, fmt.Errorf("scan pending delivery: %w", err)
		}
		d.Payload = json.RawMessage(payBytes)
		dels = append(dels, &d)
	}
	return dels, rows.Err()
}

// RecordAttempt updates a delivery after a dispatch attempt.
// success=true → status becomes 'succeeded'.
// success=false + new attempts < maxAttempts → status stays 'pending', next_attempt set.
// success=false + new attempts >= maxAttempts → status becomes 'failed'.
func (s *AlertStore) RecordAttempt(ctx context.Context, deliveryID string, success bool, nextAttempt *time.Time, maxAttempts int) error {
	// Use pgtype.Timestamptz so pgx can encode a nullable TIMESTAMPTZ.
	var na pgtype.Timestamptz
	if nextAttempt != nil {
		na = pgtype.Timestamptz{Time: *nextAttempt, Valid: true}
	}

	_, err := s.pool.Exec(ctx,
		`UPDATE webhook_deliveries
		 SET attempts     = attempts + 1,
		     last_attempt = now(),
		     status       = CASE
		                      WHEN $2 THEN 'succeeded'
		                      WHEN (attempts + 1) >= $4 THEN 'failed'
		                      ELSE 'pending'
		                    END,
		     next_attempt = CASE
		                      WHEN $2 OR (attempts + 1) >= $4 THEN NULL::timestamptz
		                      ELSE $3::timestamptz
		                    END
		 WHERE id = $1::uuid`,
		deliveryID, success, na, maxAttempts,
	)
	if err != nil {
		return fmt.Errorf("record attempt: %w", err)
	}
	return nil
}

// ListDeliveriesByEndpoint returns a page of deliveries for the given endpoint,
// scoped to orgID (verified via the endpoint lookup).
func (s *AlertStore) ListDeliveriesByEndpoint(ctx context.Context, orgID, endpointID string, limit int, cursor string) ([]*WebhookDelivery, string, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	// Verify the endpoint belongs to this org first (returns ErrWebhookEndpointNotFound if not).
	if _, err := s.GetWebhookEndpoint(ctx, orgID, endpointID); err != nil {
		return nil, "", err
	}

	var rows pgx.Rows
	var err error
	if cursor == "" {
		rows, err = s.pool.Query(ctx,
			`SELECT id, endpoint_id, event_type, payload, status, attempts, last_attempt, created_at
			 FROM webhook_deliveries WHERE endpoint_id = $1::uuid
			 ORDER BY created_at ASC, id ASC
			 LIMIT $2`,
			endpointID, limit+1,
		)
	} else {
		ct, cid, decErr := decodeCursor(cursor)
		if decErr != nil {
			return nil, "", fmt.Errorf("invalid cursor: %w", decErr)
		}
		rows, err = s.pool.Query(ctx,
			`SELECT id, endpoint_id, event_type, payload, status, attempts, last_attempt, created_at
			 FROM webhook_deliveries
			 WHERE endpoint_id = $1::uuid AND (
			   created_at > $2 OR (created_at = $2 AND id > $3::uuid)
			 )
			 ORDER BY created_at ASC, id ASC
			 LIMIT $4`,
			endpointID, ct, cid, limit+1,
		)
	}
	if err != nil {
		return nil, "", fmt.Errorf("list deliveries: %w", err)
	}
	defer rows.Close()

	var deliveries []*WebhookDelivery
	for rows.Next() {
		var d WebhookDelivery
		var payBytes []byte
		if err := rows.Scan(&d.ID, &d.EndpointID, &d.EventType, &payBytes, &d.Status, &d.Attempts, &d.LastAttempt, &d.CreatedAt); err != nil {
			return nil, "", fmt.Errorf("scan delivery: %w", err)
		}
		d.Payload = json.RawMessage(payBytes)
		deliveries = append(deliveries, &d)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("list deliveries rows: %w", err)
	}

	var nextCursor string
	if len(deliveries) > limit {
		deliveries = deliveries[:limit]
		last := deliveries[len(deliveries)-1]
		nextCursor = encodeCursor(last.CreatedAt, last.ID)
	}
	return deliveries, nextCursor, nil
}
