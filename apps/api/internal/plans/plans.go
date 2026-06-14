package plans

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/auth"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/pricing"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/respond"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/store"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/usage"
)

// Repository is the persistence interface the plans handler depends on.
// store.PlanStore satisfies it.
type Repository interface {
	CreatePlan(ctx context.Context, orgID, name string) (*store.Plan, error)
	GetPlan(ctx context.Context, orgID, planID string) (*store.Plan, error)
	CreateRateCard(ctx context.Context, planID, meterID, model string, config json.RawMessage, currency string) (*store.RateCard, error)
	ListRateCardsByPlan(ctx context.Context, orgID, planID string) ([]*store.RateCardWithMeter, error)
}

// CustomerResolver resolves a customer UUID → Customer record (for external_id lookup).
// store.CustomerStore satisfies it.
type CustomerResolver interface {
	GetCustomer(ctx context.Context, orgID, customerID string) (*store.Customer, error)
}

// MeterResolver validates that a meter UUID belongs to the org.
// store.MeterStore satisfies it.
type MeterResolver interface {
	GetMeterByID(ctx context.Context, orgID, meterID string) (*store.Meter, error)
}

// Handler groups the plan HTTP handlers.
type Handler struct {
	repo      Repository
	customers CustomerResolver
	meters    MeterResolver
	usage     usage.UsageQuerier
}

func NewHandler(repo Repository, customers CustomerResolver, meters MeterResolver, usageQuerier usage.UsageQuerier) *Handler {
	return &Handler{
		repo:      repo,
		customers: customers,
		meters:    meters,
		usage:     usageQuerier,
	}
}

// Routes registers plan routes. Expects auth middleware already applied.
func (h *Handler) Routes(r chi.Router) {
	r.Post("/", h.createPlan)
	r.Post("/{id}/rate-cards", h.createRateCard)
	r.Get("/{id}/cost", h.computeCost)
}

// planResponse is the JSON shape for a Plan per the OpenAPI contract.
type planResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt"`
}

func toPlanResponse(p *store.Plan) planResponse {
	return planResponse{
		ID:        p.ID,
		Name:      p.Name,
		CreatedAt: p.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// rateCardResponse is the JSON shape for a RateCard per the OpenAPI contract.
type rateCardResponse struct {
	ID       string          `json:"id"`
	PlanID   string          `json:"planId"`
	MeterID  string          `json:"meterId"`
	Model    string          `json:"model"`
	Config   json.RawMessage `json:"config"`
	Currency string          `json:"currency"`
}

func toRateCardResponse(rc *store.RateCard) rateCardResponse {
	return rateCardResponse{
		ID:       rc.ID,
		PlanID:   rc.PlanID,
		MeterID:  rc.MeterID,
		Model:    rc.Model,
		Config:   rc.Config,
		Currency: rc.Currency,
	}
}

var validPricingModels = map[string]bool{
	"PAYG": true, "FLAT_PLUS_OVERAGE": true, "TIERED": true,
}

// POST /v1/plans
func (h *Handler) createPlan(w http.ResponseWriter, r *http.Request) {
	orgID, ok := auth.OrgIDFromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "missing org context")
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}
	if body.Name == "" {
		respond.Error(w, http.StatusBadRequest, "missing_field", "name is required")
		return
	}

	plan, err := h.repo.CreatePlan(r.Context(), orgID, body.Name)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", "failed to create plan")
		return
	}

	respond.JSON(w, http.StatusCreated, toPlanResponse(plan))
}

// POST /v1/plans/{id}/rate-cards
func (h *Handler) createRateCard(w http.ResponseWriter, r *http.Request) {
	orgID, ok := auth.OrgIDFromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "missing org context")
		return
	}

	planID := chi.URLParam(r, "id")

	var body struct {
		MeterID  string          `json:"meterId"`
		Model    string          `json:"model"`
		Config   json.RawMessage `json:"config"`
		Currency string          `json:"currency"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}
	if body.MeterID == "" {
		respond.Error(w, http.StatusBadRequest, "missing_field", "meterId is required")
		return
	}
	if !validPricingModels[body.Model] {
		respond.Error(w, http.StatusBadRequest, "invalid_field", "model must be one of PAYG, FLAT_PLUS_OVERAGE, TIERED")
		return
	}
	if len(body.Config) == 0 {
		respond.Error(w, http.StatusBadRequest, "missing_field", "config is required")
		return
	}

	// Validate plan belongs to this org.
	if _, err := h.repo.GetPlan(r.Context(), orgID, planID); err != nil {
		if errors.Is(err, store.ErrPlanNotFound) {
			respond.Error(w, http.StatusNotFound, "not_found", "plan not found")
			return
		}
		respond.Error(w, http.StatusInternalServerError, "internal_error", "failed to look up plan")
		return
	}

	// Validate meter belongs to this org.
	if _, err := h.meters.GetMeterByID(r.Context(), orgID, body.MeterID); err != nil {
		if errors.Is(err, store.ErrMeterNotFound) {
			respond.Error(w, http.StatusNotFound, "not_found", "meter not found")
			return
		}
		respond.Error(w, http.StatusInternalServerError, "internal_error", "failed to look up meter")
		return
	}

	currency := body.Currency
	if currency == "" {
		currency = "USD"
	}

	rc, err := h.repo.CreateRateCard(r.Context(), planID, body.MeterID, body.Model, body.Config, currency)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", "failed to create rate card")
		return
	}

	respond.JSON(w, http.StatusCreated, toRateCardResponse(rc))
}

// GET /v1/plans/{id}/cost
func (h *Handler) computeCost(w http.ResponseWriter, r *http.Request) {
	orgID, ok := auth.OrgIDFromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "missing org context")
		return
	}

	planID := chi.URLParam(r, "id")
	q := r.URL.Query()

	customerID := q.Get("customerId")
	fromStr := q.Get("from")
	toStr := q.Get("to")

	if customerID == "" || fromStr == "" || toStr == "" {
		respond.Error(w, http.StatusBadRequest, "missing_field", "customerId, from, and to are required")
		return
	}

	from, err := time.Parse(time.RFC3339, fromStr)
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid_field", "from must be RFC3339")
		return
	}
	to, err := time.Parse(time.RFC3339, toStr)
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid_field", "to must be RFC3339")
		return
	}
	if !to.After(from) {
		respond.Error(w, http.StatusBadRequest, "invalid_field", "to must be after from")
		return
	}

	// Validate plan.
	if _, err := h.repo.GetPlan(r.Context(), orgID, planID); err != nil {
		if errors.Is(err, store.ErrPlanNotFound) {
			respond.Error(w, http.StatusNotFound, "not_found", "plan not found")
			return
		}
		respond.Error(w, http.StatusInternalServerError, "internal_error", "failed to look up plan")
		return
	}

	// Resolve customer → external_id for subject-based event attribution.
	customer, err := h.customers.GetCustomer(r.Context(), orgID, customerID)
	if err != nil {
		if errors.Is(err, store.ErrCustomerNotFound) {
			respond.Error(w, http.StatusNotFound, "not_found", "customer not found")
			return
		}
		respond.Error(w, http.StatusInternalServerError, "internal_error", "failed to look up customer")
		return
	}

	// Fetch rate cards with meter metadata.
	rateCards, err := h.repo.ListRateCardsByPlan(r.Context(), orgID, planID)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", "failed to list rate cards")
		return
	}

	type lineItem struct {
		Meter string  `json:"meter"`
		Usage float64 `json:"usage"`
		Cost  float64 `json:"cost"`
	}

	var lineItems []lineItem
	var totalCost float64
	currency := "USD"

	for i, rc := range rateCards {
		if i == 0 {
			currency = rc.Currency
		}

		params := usage.QueryParams{
			OrgID:      orgID,
			MeterType:  rc.MeterEventType,
			Agg:        rc.MeterAgg,
			ValueProp:  rc.MeterValueProp,
			Subject:    customer.ExternalID,
			From:       from,
			To:         to,
			WindowSize: "DAY", // not used by TotalUsage but satisfies the interface
		}

		usageAmount, err := h.usage.TotalUsage(r.Context(), params)
		if err != nil {
			respond.Error(w, http.StatusInternalServerError, "internal_error", "failed to compute usage")
			return
		}

		lineCost, err := pricing.ComputeCost(rc.Model, rc.Config, usageAmount)
		if err != nil {
			respond.Error(w, http.StatusInternalServerError, "internal_error", "failed to compute cost")
			return
		}

		lineItems = append(lineItems, lineItem{
			Meter: rc.MeterSlug,
			Usage: usageAmount,
			Cost:  lineCost,
		})
		totalCost += lineCost
	}

	if lineItems == nil {
		lineItems = []lineItem{}
	}

	type costResponse struct {
		Currency  string     `json:"currency"`
		Total     float64    `json:"total"`
		LineItems []lineItem `json:"lineItems"`
	}
	respond.JSON(w, http.StatusOK, costResponse{
		Currency:  currency,
		Total:     totalCost,
		LineItems: lineItems,
	})
}
