package customers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/auth"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/respond"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/store"
)

// Repository is the persistence interface the handlers depend on.
// store.CustomerStore satisfies it.
type Repository interface {
	CreateCustomer(ctx context.Context, orgID, externalID string, name *string, metadata json.RawMessage) (*store.Customer, error)
	GetCustomer(ctx context.Context, orgID, customerID string) (*store.Customer, error)
	ListCustomers(ctx context.Context, orgID string, limit int, cursor string) ([]*store.Customer, string, error)
	UpdateCustomer(ctx context.Context, orgID, customerID string, upd store.CustomerUpdate) (*store.Customer, error)
}

// Handler groups the customer HTTP handlers.
type Handler struct {
	repo Repository
}

func NewHandler(repo Repository) *Handler {
	return &Handler{repo: repo}
}

// Routes registers customer routes on r. Expects auth middleware already applied.
func (h *Handler) Routes(r chi.Router) {
	r.Post("/", h.create)
	r.Get("/", h.list)
	r.Get("/{id}", h.get)
	r.Patch("/{id}", h.update)
}

// customerResponse is the JSON shape for a Customer per the OpenAPI contract.
type customerResponse struct {
	ID         string          `json:"id"`
	ExternalID string          `json:"externalId"`
	Name       *string         `json:"name"`
	Metadata   json.RawMessage `json:"metadata"`
	CreatedAt  string          `json:"createdAt"`
}

func toResponse(c *store.Customer) customerResponse {
	meta := c.Metadata
	if len(meta) == 0 {
		meta = json.RawMessage("{}")
	}
	return customerResponse{
		ID:         c.ID,
		ExternalID: c.ExternalID,
		Name:       c.Name,
		Metadata:   meta,
		CreatedAt:  c.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

// POST /v1/customers
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	orgID, ok := auth.OrgIDFromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "missing org context")
		return
	}

	var body struct {
		ExternalID string          `json:"externalId"`
		Name       *string         `json:"name"`
		Metadata   json.RawMessage `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}
	if body.ExternalID == "" {
		respond.Error(w, http.StatusBadRequest, "missing_field", "externalId is required")
		return
	}

	c, err := h.repo.CreateCustomer(r.Context(), orgID, body.ExternalID, body.Name, body.Metadata)
	if err != nil {
		if errors.Is(err, store.ErrDuplicateExternalID) {
			respond.Error(w, http.StatusConflict, "conflict", "externalId already exists for this org")
			return
		}
		respond.Error(w, http.StatusInternalServerError, "internal_error", "failed to create customer")
		return
	}

	respond.JSON(w, http.StatusCreated, toResponse(c))
}

// GET /v1/customers
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

	customers, nextCursor, err := h.repo.ListCustomers(r.Context(), orgID, limit, cursor)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", "failed to list customers")
		return
	}

	items := make([]customerResponse, len(customers))
	for i, c := range customers {
		items[i] = toResponse(c)
	}

	type listResponse struct {
		NextCursor *string            `json:"nextCursor"`
		Data       []customerResponse `json:"data"`
	}
	resp := listResponse{Data: items}
	if nextCursor != "" {
		resp.NextCursor = &nextCursor
	}

	respond.JSON(w, http.StatusOK, resp)
}

// GET /v1/customers/{id}
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	orgID, ok := auth.OrgIDFromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "missing org context")
		return
	}

	customerID := chi.URLParam(r, "id")

	c, err := h.repo.GetCustomer(r.Context(), orgID, customerID)
	if err != nil {
		if errors.Is(err, store.ErrCustomerNotFound) {
			respond.Error(w, http.StatusNotFound, "not_found", "customer not found")
			return
		}
		respond.Error(w, http.StatusInternalServerError, "internal_error", "failed to get customer")
		return
	}

	respond.JSON(w, http.StatusOK, toResponse(c))
}

// PATCH /v1/customers/{id}
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	orgID, ok := auth.OrgIDFromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "missing org context")
		return
	}

	customerID := chi.URLParam(r, "id")

	// Decode into a raw map to distinguish absent fields from null fields.
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}

	var upd store.CustomerUpdate
	if nameRaw, ok := raw["name"]; ok {
		upd.SetName = true
		if string(nameRaw) != "null" {
			var s string
			if err := json.Unmarshal(nameRaw, &s); err != nil {
				respond.Error(w, http.StatusBadRequest, "invalid_field", "name must be a string or null")
				return
			}
			upd.Name = &s
		}
	}
	if metaRaw, ok := raw["metadata"]; ok {
		upd.SetMetadata = true
		upd.Metadata = json.RawMessage(metaRaw)
	}

	c, err := h.repo.UpdateCustomer(r.Context(), orgID, customerID, upd)
	if err != nil {
		if errors.Is(err, store.ErrCustomerNotFound) {
			respond.Error(w, http.StatusNotFound, "not_found", "customer not found")
			return
		}
		respond.Error(w, http.StatusInternalServerError, "internal_error", "failed to update customer")
		return
	}

	respond.JSON(w, http.StatusOK, toResponse(c))
}

