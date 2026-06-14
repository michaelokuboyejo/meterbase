// Package webhooks implements webhook endpoint registration (HTTP handler) and
// the background dispatcher that delivers pending payloads with HMAC signing,
// exponential backoff, and a per-attempt log.
//
// At-least-once delivery: a delivery may be retried on transient failure.
// Consumers MUST be idempotent; use the delivery ID in the X-MeterBase-Delivery
// header for deduplication.
package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/auth"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/respond"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/store"
)

// ─── HMAC signing ────────────────────────────────────────────────────────────

// ComputeSignature returns the HMAC-SHA256 signature of body using secret,
// formatted as "sha256=<hex>". This is the value sent in X-MeterBase-Signature.
func ComputeSignature(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// generateSecret produces a 32-byte cryptographically random hex string.
func generateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ─── Handler (HTTP) ──────────────────────────────────────────────────────────

// Repository is the persistence interface the HTTP handler depends on.
type Repository interface {
	CreateWebhookEndpoint(ctx context.Context, orgID, url, secret string) (*store.WebhookEndpoint, error)
	GetWebhookEndpoint(ctx context.Context, orgID, endpointID string) (*store.WebhookEndpoint, error)
	ListDeliveriesByEndpoint(ctx context.Context, orgID, endpointID string, limit int, cursor string) ([]*store.WebhookDelivery, string, error)
}

// Handler handles /v1/webhooks routes.
type Handler struct {
	repo Repository
}

// NewHandler constructs a webhook handler.
func NewHandler(repo Repository) *Handler {
	return &Handler{repo: repo}
}

// Routes registers webhook routes.
func (h *Handler) Routes(r chi.Router) {
	r.Post("/", h.create)
	r.Get("/{id}/deliveries", h.listDeliveries)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	orgID, ok := auth.OrgIDFromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "missing org")
		return
	}

	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if body.URL == "" {
		respond.Error(w, http.StatusBadRequest, "bad_request", "url required")
		return
	}

	secret, err := generateSecret()
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal", "could not generate secret")
		return
	}

	ep, err := h.repo.CreateWebhookEndpoint(r.Context(), orgID, body.URL, secret)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	respond.JSON(w, http.StatusCreated, map[string]any{
		"id":      ep.ID,
		"url":     ep.URL,
		"secret":  secret, // returned ONCE; not stored in plain form after this point
		"enabled": ep.Enabled,
	})
}

func (h *Handler) listDeliveries(w http.ResponseWriter, r *http.Request) {
	orgID, ok := auth.OrgIDFromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "missing org")
		return
	}

	endpointID := chi.URLParam(r, "id")

	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}

	deliveries, nextCursor, err := h.repo.ListDeliveriesByEndpoint(
		r.Context(), orgID, endpointID, limit, r.URL.Query().Get("cursor"),
	)
	if err != nil {
		if errors.Is(err, store.ErrWebhookEndpointNotFound) {
			respond.Error(w, http.StatusNotFound, "not_found", "webhook endpoint not found")
			return
		}
		respond.Error(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	items := make([]any, len(deliveries))
	for i, d := range deliveries {
		items[i] = deliveryResponse(d)
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

type deliveryJSON struct {
	ID          string          `json:"id"`
	EndpointID  string          `json:"endpointId"`
	EventType   string          `json:"eventType"`
	Payload     json.RawMessage `json:"payload"`
	Status      string          `json:"status"`
	Attempts    int             `json:"attempts"`
	LastAttempt *string         `json:"lastAttempt"`
	CreatedAt   string          `json:"createdAt"`
}

func deliveryResponse(d *store.WebhookDelivery) deliveryJSON {
	dj := deliveryJSON{
		ID:         d.ID,
		EndpointID: d.EndpointID,
		EventType:  d.EventType,
		Payload:    d.Payload,
		Status:     d.Status,
		Attempts:   d.Attempts,
		CreatedAt:  d.CreatedAt.UTC().Format(time.RFC3339),
	}
	if d.LastAttempt != nil {
		s := d.LastAttempt.UTC().Format(time.RFC3339)
		dj.LastAttempt = &s
	}
	return dj
}

// ─── Dispatcher (background worker) ─────────────────────────────────────────

// DispatcherRepo is the persistence interface the dispatcher worker depends on.
type DispatcherRepo interface {
	ClaimPendingDeliveries(ctx context.Context, limit int) ([]*store.WebhookDeliveryWithEndpoint, error)
	RecordAttempt(ctx context.Context, deliveryID string, success bool, nextAttempt *time.Time, maxAttempts int) error
}

// Dispatcher polls for pending deliveries and sends them via HTTP.
type Dispatcher struct {
	repo        DispatcherRepo
	client      *http.Client
	maxAttempts int
	interval    time.Duration
	log         *slog.Logger
}

// NewDispatcher constructs a dispatcher.
// maxAttempts caps how many times a failing delivery is retried.
func NewDispatcher(repo DispatcherRepo, client *http.Client, maxAttempts int) *Dispatcher {
	return &Dispatcher{
		repo:        repo,
		client:      client,
		maxAttempts: maxAttempts,
		interval:    5 * time.Second,
		log:         slog.Default(),
	}
}

// Run starts the dispatch loop; blocks until ctx is cancelled.
func (d *Dispatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.Dispatch(ctx)
		}
	}
}

// Dispatch claims all due pending deliveries and attempts each one synchronously.
// Exported so tests can drive it directly without a running ticker.
func (d *Dispatcher) Dispatch(ctx context.Context) {
	deliveries, err := d.repo.ClaimPendingDeliveries(ctx, 100)
	if err != nil {
		d.log.Error("dispatcher: claim pending", "error", err)
		return
	}
	for _, del := range deliveries {
		d.send(ctx, del)
	}
}

func (d *Dispatcher) send(ctx context.Context, del *store.WebhookDeliveryWithEndpoint) {
	body := []byte(del.Payload)
	sig := ComputeSignature([]byte(del.Secret), body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, del.URL, bytes.NewReader(body))
	if err != nil {
		d.log.Error("dispatcher: build request", "deliveryID", del.ID, "error", err)
		d.repo.RecordAttempt(ctx, del.ID, false, nil, d.maxAttempts) //nolint:errcheck
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MeterBase-Signature", sig)
	req.Header.Set("X-MeterBase-Delivery", del.ID)

	resp, err := d.client.Do(req)
	success := err == nil && resp.StatusCode >= 200 && resp.StatusCode < 300
	if resp != nil {
		resp.Body.Close()
	}

	var nextAttempt *time.Time
	if !success && del.Attempts+1 < d.maxAttempts {
		// Exponential backoff: base=30s, doubles each retry.
		delay := 30 * time.Second * (1 << uint(del.Attempts))
		t := time.Now().Add(delay)
		nextAttempt = &t
	}

	if err := d.repo.RecordAttempt(ctx, del.ID, success, nextAttempt, d.maxAttempts); err != nil {
		d.log.Error("dispatcher: record attempt", "deliveryID", del.ID, "error", err)
	}
}
