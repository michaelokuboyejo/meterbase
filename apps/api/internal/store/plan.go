package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrPlanNotFound = errors.New("plan not found")

// Plan is a pricing plan belonging to an org.
type Plan struct {
	ID        string
	OrgID     string
	Name      string
	CreatedAt time.Time
}

// RateCard binds a meter to a pricing model within a plan.
type RateCard struct {
	ID       string
	PlanID   string
	MeterID  string
	Model    string
	Config   json.RawMessage
	Currency string
}

// RateCardWithMeter extends RateCard with the meter metadata needed for cost
// computation, avoiding a second round-trip to the meters table.
type RateCardWithMeter struct {
	RateCard
	MeterSlug      string
	MeterEventType string
	MeterAgg       string
	MeterValueProp *string
}

// PlanStore handles persistence for plans and rate cards.
type PlanStore struct {
	pool *pgxpool.Pool
}

func NewPlanStore(pool *pgxpool.Pool) *PlanStore {
	return &PlanStore{pool: pool}
}

// CreatePlan inserts a new plan scoped to orgID and returns it.
func (s *PlanStore) CreatePlan(ctx context.Context, orgID, name string) (*Plan, error) {
	var p Plan
	err := s.pool.QueryRow(ctx,
		`INSERT INTO plans (org_id, name) VALUES ($1, $2)
		 RETURNING id, org_id, name, created_at`,
		orgID, name,
	).Scan(&p.ID, &p.OrgID, &p.Name, &p.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create plan: %w", err)
	}
	return &p, nil
}

// GetPlan fetches a plan by ID scoped to orgID.
// Returns ErrPlanNotFound when the plan does not exist or belongs to a different org.
func (s *PlanStore) GetPlan(ctx context.Context, orgID, planID string) (*Plan, error) {
	var p Plan
	err := s.pool.QueryRow(ctx,
		`SELECT id, org_id, name, created_at FROM plans WHERE org_id=$1 AND id=$2::uuid`,
		orgID, planID,
	).Scan(&p.ID, &p.OrgID, &p.Name, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPlanNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get plan: %w", err)
	}
	return &p, nil
}

// CreateRateCard inserts a rate card into the given plan.
// The caller must validate that the plan and meter both belong to the org.
func (s *PlanStore) CreateRateCard(ctx context.Context, planID, meterID, model string, config json.RawMessage, currency string) (*RateCard, error) {
	if currency == "" {
		currency = "USD"
	}
	var rc RateCard
	err := s.pool.QueryRow(ctx,
		`INSERT INTO rate_cards (plan_id, meter_id, model, config, currency)
		 VALUES ($1::uuid, $2::uuid, $3::pricing_model, $4, $5)
		 RETURNING id, plan_id, meter_id, model::text, config, currency`,
		planID, meterID, model, config, currency,
	).Scan(&rc.ID, &rc.PlanID, &rc.MeterID, &rc.Model, &rc.Config, &rc.Currency)
	if err != nil {
		return nil, fmt.Errorf("create rate card: %w", err)
	}
	return &rc, nil
}

// ListRateCardsByPlan returns all rate cards for a plan, joined with meter metadata.
// Tenancy is enforced by joining through plans.org_id = orgID.
func (s *PlanStore) ListRateCardsByPlan(ctx context.Context, orgID, planID string) ([]*RateCardWithMeter, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT rc.id, rc.plan_id, rc.meter_id, rc.model::text, rc.config, rc.currency,
		        m.slug, m.event_type, m.agg::text, m.value_prop
		 FROM rate_cards rc
		 JOIN meters m ON m.id = rc.meter_id
		 JOIN plans  p ON p.id = rc.plan_id
		 WHERE p.org_id = $1 AND p.id = $2::uuid
		 ORDER BY rc.id`,
		orgID, planID,
	)
	if err != nil {
		return nil, fmt.Errorf("list rate cards: %w", err)
	}
	defer rows.Close()

	var results []*RateCardWithMeter
	for rows.Next() {
		var rcm RateCardWithMeter
		if err := rows.Scan(
			&rcm.ID, &rcm.PlanID, &rcm.MeterID, &rcm.Model, &rcm.Config, &rcm.Currency,
			&rcm.MeterSlug, &rcm.MeterEventType, &rcm.MeterAgg, &rcm.MeterValueProp,
		); err != nil {
			return nil, fmt.Errorf("scan rate card: %w", err)
		}
		results = append(results, &rcm)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rate card rows: %w", err)
	}
	return results, nil
}
