// Package alerts implements alert rule CRUD (HTTP handler) and the background
// evaluator that detects threshold crossings and enqueues webhook deliveries.
//
// Webhook consumers should treat deliveries as idempotent: the same alert crossing
// may be retried if a delivery fails. Use the delivery ID for deduplication on
// the consumer side.
package alerts

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/auth"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/respond"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/store"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/usage"
)

// ─── Handler (HTTP) ──────────────────────────────────────────────────────────

// AlertRepository is the persistence interface the HTTP handler depends on.
type AlertRepository interface {
	CreateAlertRule(ctx context.Context, orgID string, in store.AlertRuleInput) (*store.AlertRule, error)
	ListAlertRules(ctx context.Context, orgID string, limit int, cursor string) ([]*store.AlertRule, string, error)
}

// MeterResolver validates that a meter exists and belongs to the org.
type MeterResolver interface {
	GetMeterByID(ctx context.Context, orgID, meterID string) (*store.Meter, error)
}

// Handler handles /v1/alert-rules routes.
type Handler struct {
	repo   AlertRepository
	meters MeterResolver
}

// NewHandler constructs an alert rule handler.
func NewHandler(repo AlertRepository, meters MeterResolver) *Handler {
	return &Handler{repo: repo, meters: meters}
}

// Routes registers alert-rule routes.
func (h *Handler) Routes(r chi.Router) {
	r.Post("/", h.create)
	r.Get("/", h.list)
}

type createBody struct {
	MeterID   string   `json:"meterId"`
	Scope     string   `json:"scope"`
	Window    string   `json:"window"`
	Threshold *float64 `json:"threshold"`
	Enabled   *bool    `json:"enabled"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	orgID, ok := auth.OrgIDFromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "missing org")
		return
	}

	var body createBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}

	if body.MeterID == "" || body.Scope == "" || body.Window == "" || body.Threshold == nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", "meterId, scope, window, threshold required")
		return
	}

	validScopes := map[string]bool{"subject": true, "customer": true, "global": true}
	if !validScopes[body.Scope] {
		respond.Error(w, http.StatusBadRequest, "bad_request", "scope must be subject, customer, or global")
		return
	}

	validWindows := map[string]bool{"MINUTE": true, "HOUR": true, "DAY": true, "MONTH": true}
	if !validWindows[body.Window] {
		respond.Error(w, http.StatusBadRequest, "bad_request", "window must be MINUTE, HOUR, DAY, or MONTH")
		return
	}

	if _, err := h.meters.GetMeterByID(r.Context(), orgID, body.MeterID); err != nil {
		respond.Error(w, http.StatusNotFound, "not_found", "meter not found")
		return
	}

	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}

	rule, err := h.repo.CreateAlertRule(r.Context(), orgID, store.AlertRuleInput{
		MeterID:   body.MeterID,
		Scope:     body.Scope,
		Window:    body.Window,
		Threshold: *body.Threshold,
		Enabled:   enabled,
	})
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	respond.JSON(w, http.StatusCreated, alertRuleResponse(rule))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	orgID, ok := auth.OrgIDFromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "missing org")
		return
	}

	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}

	rules, nextCursor, err := h.repo.ListAlertRules(r.Context(), orgID, limit, r.URL.Query().Get("cursor"))
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	items := make([]any, len(rules))
	for i, ru := range rules {
		items[i] = alertRuleResponse(ru)
	}

	var nc *string
	if nextCursor != "" {
		nc = &nextCursor
	}
	respond.JSON(w, http.StatusOK, map[string]any{
		"data":       items,
		"nextCursor": nc,
	})
}

type alertRuleJSON struct {
	ID        string  `json:"id"`
	MeterID   string  `json:"meterId"`
	Scope     string  `json:"scope"`
	Window    string  `json:"window"`
	Threshold float64 `json:"threshold"`
	Enabled   bool    `json:"enabled"`
	CreatedAt string  `json:"createdAt"`
}

func alertRuleResponse(r *store.AlertRule) alertRuleJSON {
	return alertRuleJSON{
		ID:        r.ID,
		MeterID:   r.MeterID,
		Scope:     r.Scope,
		Window:    r.Window,
		Threshold: r.Threshold,
		Enabled:   r.Enabled,
		CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// ─── Evaluator (background worker) ──────────────────────────────────────────

// EvaluatorRepo is the persistence interface the evaluator worker depends on.
type EvaluatorRepo interface {
	LoadEnabledRules(ctx context.Context) ([]*store.AlertRuleWithMeter, error)
	GetDistinctScopeKeys(ctx context.Context, orgID, eventType, scopeType string, from, to time.Time) ([]string, error)
	TryRecordFiring(ctx context.Context, ruleID, scopeKey string, windowStart time.Time) (bool, error)
	ListEnabledEndpointsByOrg(ctx context.Context, orgID string) ([]*store.WebhookEndpoint, error)
	CreateDelivery(ctx context.Context, endpointID, eventType string, payload json.RawMessage) (*store.WebhookDelivery, error)
}

// Evaluator periodically checks alert rules and creates webhook deliveries
// on the first threshold crossing in each window period (de-bounce via alert_firings).
type Evaluator struct {
	repo     EvaluatorRepo
	usage    usage.UsageQuerier
	interval time.Duration
	log      *slog.Logger
}

// NewEvaluator constructs an evaluator that checks rules every minute.
func NewEvaluator(repo EvaluatorRepo, uq usage.UsageQuerier) *Evaluator {
	return &Evaluator{
		repo:     repo,
		usage:    uq,
		interval: time.Minute,
		log:      slog.Default(),
	}
}

// Run starts the evaluation loop; blocks until ctx is cancelled.
func (e *Evaluator) Run(ctx context.Context) {
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			e.Tick(ctx, t)
		}
	}
}

// Tick evaluates all enabled rules as of now. Exported so tests can call it
// directly with a controlled clock.
func (e *Evaluator) Tick(ctx context.Context, now time.Time) {
	rules, err := e.repo.LoadEnabledRules(ctx)
	if err != nil {
		e.log.Error("evaluator: load rules", "error", err)
		return
	}
	for _, rule := range rules {
		e.evalRule(ctx, rule, now)
	}
}

func (e *Evaluator) evalRule(ctx context.Context, rule *store.AlertRuleWithMeter, now time.Time) {
	windowStart := computeWindowStart(rule.Window, now)

	var scopeKeys []string
	if rule.Scope == "global" {
		scopeKeys = []string{""}
	} else {
		var err error
		scopeKeys, err = e.repo.GetDistinctScopeKeys(ctx, rule.OrgID, rule.MeterEventType, rule.Scope, windowStart, now)
		if err != nil {
			e.log.Error("evaluator: get scope keys", "ruleID", rule.ID, "error", err)
			return
		}
	}

	for _, sk := range scopeKeys {
		e.evalScope(ctx, rule, now, windowStart, sk)
	}
}

func (e *Evaluator) evalScope(ctx context.Context, rule *store.AlertRuleWithMeter, now, windowStart time.Time, scopeKey string) {
	params := usage.QueryParams{
		OrgID:      rule.OrgID,
		MeterType:  rule.MeterEventType,
		Agg:        rule.MeterAgg,
		ValueProp:  rule.MeterValueProp,
		From:       windowStart,
		To:         now,
		WindowSize: "HOUR",
	}
	switch rule.Scope {
	case "subject":
		params.Subject = scopeKey
	case "customer":
		params.CustomerID = scopeKey
	}

	total, err := e.usage.TotalUsage(ctx, params)
	if err != nil {
		e.log.Error("evaluator: total usage", "ruleID", rule.ID, "scopeKey", scopeKey, "error", err)
		return
	}

	if total < rule.Threshold {
		return
	}

	fired, err := e.repo.TryRecordFiring(ctx, rule.ID, scopeKey, windowStart)
	if err != nil {
		e.log.Error("evaluator: try record firing", "ruleID", rule.ID, "error", err)
		return
	}
	if !fired {
		return // already fired this window period — de-bounce
	}

	payload, err := buildPayload(rule, scopeKey, total, now)
	if err != nil {
		e.log.Error("evaluator: build payload", "error", err)
		return
	}

	endpoints, err := e.repo.ListEnabledEndpointsByOrg(ctx, rule.OrgID)
	if err != nil {
		e.log.Error("evaluator: list endpoints", "orgID", rule.OrgID, "error", err)
		return
	}

	for _, ep := range endpoints {
		if _, err := e.repo.CreateDelivery(ctx, ep.ID, "alert.triggered", payload); err != nil {
			e.log.Error("evaluator: create delivery", "endpointID", ep.ID, "error", err)
		}
	}
}

type alertPayload struct {
	Type        string  `json:"type"`
	AlertRuleID string  `json:"alert_rule_id"`
	Meter       string  `json:"meter"`
	Subject     string  `json:"subject,omitempty"`
	Value       float64 `json:"value"`
	Threshold   float64 `json:"threshold"`
	Window      string  `json:"window"`
	OccurredAt  string  `json:"occurred_at"`
}

func buildPayload(rule *store.AlertRuleWithMeter, scopeKey string, value float64, now time.Time) (json.RawMessage, error) {
	p := alertPayload{
		Type:        "alert.triggered",
		AlertRuleID: rule.ID,
		Meter:       rule.MeterSlug,
		Value:       value,
		Threshold:   rule.Threshold,
		Window:      rule.Window,
		OccurredAt:  now.UTC().Format(time.RFC3339),
	}
	if rule.Scope != "global" {
		p.Subject = scopeKey
	}
	return json.Marshal(p)
}

// computeWindowStart truncates now to the current window boundary.
func computeWindowStart(window string, now time.Time) time.Time {
	now = now.UTC()
	switch window {
	case "MINUTE":
		return now.Truncate(time.Minute)
	case "HOUR":
		return now.Truncate(time.Hour)
	case "DAY":
		y, m, d := now.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	case "MONTH":
		y, m, _ := now.Date()
		return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	default:
		return now.Truncate(time.Hour)
	}
}
