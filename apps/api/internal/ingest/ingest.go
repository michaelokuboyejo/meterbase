package ingest

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/auth"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/respond"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/store"
)

// EventStore is the persistence interface the handler depends on.
// store.TimescaleEventStore satisfies it.
type EventStore interface {
	IngestEvents(ctx context.Context, orgID string, events []store.IngestEvent) (received, duplicates int, err error)
}

// EventListStore is the read interface for listing and exporting events.
// store.TimescaleEventStore satisfies it after Phase 4 additions.
type EventListStore interface {
	ListEvents(ctx context.Context, orgID string, p store.ListEventsParams) ([]store.StoredEvent, string, error)
	ExportEvents(ctx context.Context, orgID string, p store.ListEventsParams) ([]store.StoredEvent, error)
}

// Handler handles event ingestion and listing requests.
type Handler struct {
	store     EventStore
	listStore EventListStore
}

func NewHandler(es EventStore, ls EventListStore) *Handler {
	return &Handler{store: es, listStore: ls}
}

// Routes registers event routes on r.
// Expects auth middleware already applied.
func (h *Handler) Routes(r chi.Router) {
	r.Post("/", h.ingest)
	r.Get("/", h.list)
	r.Get("/export", h.export)
}

// eventInput is the JSON shape for an incoming event per the OpenAPI contract.
type eventInput struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Source  string          `json:"source"`
	Subject string          `json:"subject"`
	Time    *time.Time      `json:"time"`
	Data    json.RawMessage `json:"data"`
}

// storedEventJSON is the camelCase JSON shape for a StoredEvent per the contract.
type storedEventJSON struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Source     string          `json:"source,omitempty"`
	Subject    string          `json:"subject"`
	Time       time.Time       `json:"time"`
	IngestedAt time.Time       `json:"ingestedAt"`
	CustomerID *string         `json:"customerId"`
	Data       json.RawMessage `json:"data"`
}

func toStoredEventJSON(e store.StoredEvent) storedEventJSON {
	return storedEventJSON{
		ID:         e.ID,
		Type:       e.Type,
		Source:     e.Source,
		Subject:    e.Subject,
		Time:       e.Time.UTC(),
		IngestedAt: e.IngestedAt.UTC(),
		CustomerID: e.CustomerID,
		Data:       e.Data,
	}
}

// POST /v1/events — accepts a single Event OR an array of Events.
func (h *Handler) ingest(w http.ResponseWriter, r *http.Request) {
	orgID, ok := auth.OrgIDFromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "missing org context")
		return
	}

	var raw json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}

	var inputs []eventInput
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &inputs); err != nil {
			respond.Error(w, http.StatusBadRequest, "invalid_json", "invalid event array")
			return
		}
	} else {
		var single eventInput
		if err := json.Unmarshal(trimmed, &single); err != nil {
			respond.Error(w, http.StatusBadRequest, "invalid_json", "invalid event object")
			return
		}
		inputs = []eventInput{single}
	}

	if len(inputs) == 0 {
		respond.Error(w, http.StatusBadRequest, "invalid_request", "at least one event is required")
		return
	}

	// Validate required fields on every event.
	for i, e := range inputs {
		if e.ID == "" {
			respond.Error(w, http.StatusBadRequest, "missing_field", eventFieldErr(i, "id"))
			return
		}
		if e.Type == "" {
			respond.Error(w, http.StatusBadRequest, "missing_field", eventFieldErr(i, "type"))
			return
		}
		if e.Subject == "" {
			respond.Error(w, http.StatusBadRequest, "missing_field", eventFieldErr(i, "subject"))
			return
		}
	}

	// Convert to store type.
	storeEvents := make([]store.IngestEvent, len(inputs))
	for i, e := range inputs {
		storeEvents[i] = store.IngestEvent{
			ID:      e.ID,
			Type:    e.Type,
			Source:  e.Source,
			Subject: e.Subject,
			Time:    e.Time,
			Data:    e.Data,
		}
	}

	received, duplicates, err := h.store.IngestEvents(r.Context(), orgID, storeEvents)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", "failed to ingest events")
		return
	}

	respond.JSON(w, http.StatusAccepted, map[string]int{
		"received":   received,
		"duplicates": duplicates,
	})
}

// GET /v1/events — paginated list of raw events.
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	orgID, ok := auth.OrgIDFromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "missing org context")
		return
	}

	q := r.URL.Query()
	p := store.ListEventsParams{
		Subject: q.Get("subject"),
		Type:    q.Get("type"),
		Cursor:  q.Get("cursor"),
		Limit:   100,
	}

	if s := q.Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			p.Limit = n
		}
	}
	if s := q.Get("from"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			p.From = &t
		}
	}
	if s := q.Get("to"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			p.To = &t
		}
	}

	events, nextCursor, err := h.listStore.ListEvents(r.Context(), orgID, p)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", "failed to list events")
		return
	}

	out := make([]storedEventJSON, len(events))
	for i, e := range events {
		out[i] = toStoredEventJSON(e)
	}

	type listResponse struct {
		NextCursor *string          `json:"nextCursor"`
		Data       []storedEventJSON `json:"data"`
	}
	var nc *string
	if nextCursor != "" {
		nc = &nextCursor
	}
	respond.JSON(w, http.StatusOK, listResponse{NextCursor: nc, Data: out})
}

// GET /v1/events/export — stream all matching events as CSV or JSON.
func (h *Handler) export(w http.ResponseWriter, r *http.Request) {
	orgID, ok := auth.OrgIDFromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "missing org context")
		return
	}

	q := r.URL.Query()
	p := store.ListEventsParams{
		Subject: q.Get("subject"),
		Type:    q.Get("type"),
	}
	if s := q.Get("from"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			p.From = &t
		}
	}
	if s := q.Get("to"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			p.To = &t
		}
	}

	format := q.Get("format")
	if format == "" {
		format = "json"
	}

	events, err := h.listStore.ExportEvents(r.Context(), orgID, p)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", "failed to export events")
		return
	}

	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv")
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"id", "type", "source", "subject", "time", "ingestedAt", "customerId", "data"})
		for _, e := range events {
			cid := ""
			if e.CustomerID != nil {
				cid = *e.CustomerID
			}
			_ = cw.Write([]string{
				e.ID, e.Type, e.Source, e.Subject,
				e.Time.UTC().Format(time.RFC3339),
				e.IngestedAt.UTC().Format(time.RFC3339),
				cid, string(e.Data),
			})
		}
		cw.Flush()
		return
	}

	out := make([]storedEventJSON, len(events))
	for i, e := range events {
		out[i] = toStoredEventJSON(e)
	}
	respond.JSON(w, http.StatusOK, out)
}

func eventFieldErr(idx int, field string) string {
	return "event[" + itoa(idx) + "]." + field + " is required"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
