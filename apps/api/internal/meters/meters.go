package meters

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/auth"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/respond"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/store"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/usage"
)

// Repository is the persistence interface the handlers depend on.
// store.MeterStore satisfies it.
type Repository interface {
	CreateMeter(ctx context.Context, orgID, slug, eventType, agg string, valueProp *string, groupBy []string) (*store.Meter, error)
	GetMeterBySlug(ctx context.Context, orgID, slug string) (*store.Meter, error)
	GetMeterByID(ctx context.Context, orgID, meterID string) (*store.Meter, error)
	ListMeters(ctx context.Context, orgID string, limit int, cursor string) ([]*store.Meter, string, error)
	DeleteMeter(ctx context.Context, orgID, slug string) error
}

// Handler groups the meter HTTP handlers.
type Handler struct {
	repo  Repository
	usage usage.UsageQuerier
}

func NewHandler(repo Repository, usageRepo usage.UsageQuerier) *Handler {
	return &Handler{repo: repo, usage: usageRepo}
}

// Routes registers meter routes on r. Expects auth middleware already applied.
func (h *Handler) Routes(r chi.Router) {
	r.Post("/", h.create)
	r.Get("/", h.list)
	r.Get("/{slug}/query", h.query)
	r.Get("/{slug}", h.get)
	r.Delete("/{slug}", h.delete)
}

// meterResponse is the JSON shape for a Meter per the OpenAPI contract.
type meterResponse struct {
	ID            string   `json:"id"`
	Slug          string   `json:"slug"`
	EventType     string   `json:"eventType"`
	Aggregation   string   `json:"aggregation"`
	ValueProperty *string  `json:"valueProperty"`
	GroupBy       []string `json:"groupBy"`
	CreatedAt     string   `json:"createdAt"`
}

func toMeterResponse(m *store.Meter) meterResponse {
	gb := m.GroupBy
	if gb == nil {
		gb = []string{}
	}
	return meterResponse{
		ID:            m.ID,
		Slug:          m.Slug,
		EventType:     m.EventType,
		Aggregation:   m.Aggregation,
		ValueProperty: m.ValueProperty,
		GroupBy:       gb,
		CreatedAt:     m.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

// POST /v1/meters
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	orgID, ok := auth.OrgIDFromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "missing org context")
		return
	}

	var body struct {
		Slug          string   `json:"slug"`
		EventType     string   `json:"eventType"`
		Aggregation   string   `json:"aggregation"`
		ValueProperty *string  `json:"valueProperty"`
		GroupBy       []string `json:"groupBy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}
	if body.Slug == "" {
		respond.Error(w, http.StatusBadRequest, "missing_field", "slug is required")
		return
	}
	if body.EventType == "" {
		respond.Error(w, http.StatusBadRequest, "missing_field", "eventType is required")
		return
	}
	if !validAggregation(body.Aggregation) {
		respond.Error(w, http.StatusBadRequest, "invalid_field", "aggregation must be one of SUM, COUNT, AVG, MIN, MAX, UNIQUE_COUNT")
		return
	}

	m, err := h.repo.CreateMeter(r.Context(), orgID, body.Slug, body.EventType, body.Aggregation, body.ValueProperty, body.GroupBy)
	if err != nil {
		if errors.Is(err, store.ErrDuplicateMeterSlug) {
			respond.Error(w, http.StatusConflict, "conflict", "meter slug already exists for this org")
			return
		}
		respond.Error(w, http.StatusInternalServerError, "internal_error", "failed to create meter")
		return
	}

	respond.JSON(w, http.StatusCreated, toMeterResponse(m))
}

// GET /v1/meters
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	orgID, ok := auth.OrgIDFromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "missing org context")
		return
	}

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 1000 {
			limit = v
		}
	}
	cursor := r.URL.Query().Get("cursor")

	meters, nextCursor, err := h.repo.ListMeters(r.Context(), orgID, limit, cursor)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", "failed to list meters")
		return
	}

	items := make([]meterResponse, len(meters))
	for i, m := range meters {
		items[i] = toMeterResponse(m)
	}

	type listResponse struct {
		NextCursor *string         `json:"nextCursor"`
		Data       []meterResponse `json:"data"`
	}
	resp := listResponse{Data: items}
	if nextCursor != "" {
		resp.NextCursor = &nextCursor
	}
	respond.JSON(w, http.StatusOK, resp)
}

// GET /v1/meters/{slug}
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	orgID, ok := auth.OrgIDFromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "missing org context")
		return
	}

	slug := chi.URLParam(r, "slug")
	m, err := h.repo.GetMeterBySlug(r.Context(), orgID, slug)
	if err != nil {
		if errors.Is(err, store.ErrMeterNotFound) {
			respond.Error(w, http.StatusNotFound, "not_found", "meter not found")
			return
		}
		respond.Error(w, http.StatusInternalServerError, "internal_error", "failed to get meter")
		return
	}

	respond.JSON(w, http.StatusOK, toMeterResponse(m))
}

// DELETE /v1/meters/{slug}
func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	orgID, ok := auth.OrgIDFromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "missing org context")
		return
	}

	slug := chi.URLParam(r, "slug")
	err := h.repo.DeleteMeter(r.Context(), orgID, slug)
	if err != nil {
		if errors.Is(err, store.ErrMeterNotFound) {
			respond.Error(w, http.StatusNotFound, "not_found", "meter not found")
			return
		}
		respond.Error(w, http.StatusInternalServerError, "internal_error", "failed to delete meter")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GET /v1/meters/{slug}/query
func (h *Handler) query(w http.ResponseWriter, r *http.Request) {
	orgID, ok := auth.OrgIDFromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "missing org context")
		return
	}

	q := r.URL.Query()
	slug := chi.URLParam(r, "slug")

	fromStr := q.Get("from")
	toStr := q.Get("to")
	windowSize := q.Get("windowSize")

	if fromStr == "" || toStr == "" || windowSize == "" {
		respond.Error(w, http.StatusBadRequest, "missing_field", "from, to, and windowSize are required")
		return
	}
	if !validWindowSize(windowSize) {
		respond.Error(w, http.StatusBadRequest, "invalid_field", "windowSize must be MINUTE, HOUR, DAY, or MONTH")
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

	m, err := h.repo.GetMeterBySlug(r.Context(), orgID, slug)
	if err != nil {
		if errors.Is(err, store.ErrMeterNotFound) {
			respond.Error(w, http.StatusNotFound, "not_found", "meter not found")
			return
		}
		respond.Error(w, http.StatusInternalServerError, "internal_error", "failed to get meter")
		return
	}

	// Validate requested groupBy dims are a subset of the meter's declared groupBy.
	reqGroupBy := q["groupBy"]
	if len(reqGroupBy) > 0 {
		allowed := make(map[string]bool, len(m.GroupBy))
		for _, d := range m.GroupBy {
			allowed[d] = true
		}
		for _, d := range reqGroupBy {
			if !allowed[d] {
				respond.Error(w, http.StatusBadRequest, "invalid_field", "groupBy dimension not declared on meter: "+d)
				return
			}
		}
	}

	params := usage.QueryParams{
		OrgID:      orgID,
		MeterSlug:  m.Slug,
		MeterID:    m.ID,
		MeterType:  m.EventType,
		Agg:        m.Aggregation,
		ValueProp:  m.ValueProperty,
		GroupBy:    reqGroupBy,
		From:       from,
		To:         to,
		WindowSize: windowSize,
		Subject:    q.Get("subject"),
		CustomerID: q.Get("customerId"),
	}

	result, err := h.usage.QueryUsage(r.Context(), params)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", "query failed")
		return
	}

	respond.JSON(w, http.StatusOK, toQueryResultJSON(result))
}

type dataPointJSON struct {
	Bucket string            `json:"bucket"`
	Value  float64           `json:"value"`
	Groups map[string]string `json:"groups,omitempty"`
}

type queryResultJSON struct {
	Meter      string          `json:"meter"`
	WindowSize string          `json:"windowSize"`
	Data       []dataPointJSON `json:"data"`
}

func toQueryResultJSON(r *usage.QueryResult) queryResultJSON {
	data := make([]dataPointJSON, len(r.Data))
	for i, dp := range r.Data {
		data[i] = dataPointJSON{
			Bucket: dp.Bucket.UTC().Format(time.RFC3339),
			Value:  dp.Value,
			Groups: dp.Groups,
		}
	}
	return queryResultJSON{
		Meter:      r.Meter,
		WindowSize: r.WindowSize,
		Data:       data,
	}
}

var validAggregations = map[string]bool{
	"SUM": true, "COUNT": true, "AVG": true,
	"MIN": true, "MAX": true, "UNIQUE_COUNT": true,
}

func validAggregation(a string) bool {
	return validAggregations[a]
}

var validWindowSizes = map[string]bool{
	"MINUTE": true, "HOUR": true, "DAY": true, "MONTH": true,
}

func validWindowSize(ws string) bool {
	return validWindowSizes[ws]
}
