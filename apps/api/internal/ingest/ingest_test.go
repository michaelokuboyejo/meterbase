package ingest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/auth"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/store"
)

// mockKeyResolver always resolves to a fixed orgID.
type mockKeyResolver struct{ orgID string }

func (m *mockKeyResolver) ResolveKey(_ context.Context, _ string) (string, error) {
	return m.orgID, nil
}

// mockEventStore is an in-memory stub satisfying EventStore.
type mockEventStore struct {
	calls [][]store.IngestEvent // each Ingest call's events slice
	// Controls what received/duplicates to return per call index.
	received   []int
	duplicates []int
}

func (m *mockEventStore) IngestEvents(_ context.Context, _ string, events []store.IngestEvent) (int, int, error) {
	idx := len(m.calls)
	m.calls = append(m.calls, events)
	rec, dup := 0, 0
	if idx < len(m.received) {
		rec = m.received[idx]
		dup = m.duplicates[idx]
	} else {
		rec = len(events)
	}
	return rec, dup, nil
}

func buildIngestRouter(orgID string, h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(auth.Middleware(&mockKeyResolver{orgID: orgID}))
	r.Route("/v1/events", h.Routes)
	return r
}

func doIngest(r http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/events", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// ── DEDUP response shape ──────────────────────────────────────────────────────

// When the mock store reports received=1 dup=1, the handler returns that.
func TestIngestHandler_DedupResponse(t *testing.T) {
	ms := &mockEventStore{received: []int{1}, duplicates: []int{1}}
	h := NewHandler(ms, nil)
	r := buildIngestRouter("org-1", h)

	body := `{"id":"e1","type":"api_req","subject":"u1"}`
	rec := doIngest(r, body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]int
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["received"] != 1 || resp["duplicates"] != 1 {
		t.Errorf("want received=1 dup=1, got %v", resp)
	}
}

// ── Single event ─────────────────────────────────────────────────────────────

func TestIngestHandler_SingleEvent(t *testing.T) {
	ms := &mockEventStore{}
	h := NewHandler(ms, nil)
	r := buildIngestRouter("org-1", h)

	body := `{"id":"single-1","type":"api_req","subject":"user_1","data":{"tokens":500}}`
	rec := doIngest(r, body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(ms.calls) != 1 || len(ms.calls[0]) != 1 {
		t.Errorf("expected 1 call with 1 event, got %v", ms.calls)
	}
	if ms.calls[0][0].ID != "single-1" {
		t.Errorf("id: want single-1, got %s", ms.calls[0][0].ID)
	}
}

// ── Batch event ───────────────────────────────────────────────────────────────

func TestIngestHandler_BatchEvents(t *testing.T) {
	ms := &mockEventStore{}
	h := NewHandler(ms, nil)
	r := buildIngestRouter("org-1", h)

	body := `[{"id":"b1","type":"t","subject":"u"},{"id":"b2","type":"t","subject":"u"}]`
	rec := doIngest(r, body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(ms.calls) != 1 || len(ms.calls[0]) != 2 {
		t.Errorf("expected 1 call with 2 events, got %v", ms.calls)
	}
}

// ── Validation ────────────────────────────────────────────────────────────────

func TestIngestHandler_MissingID(t *testing.T) {
	ms := &mockEventStore{}
	h := NewHandler(ms, nil)
	r := buildIngestRouter("org-1", h)
	rec := doIngest(r, `{"type":"t","subject":"u"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rec.Code)
	}
}

func TestIngestHandler_MissingType(t *testing.T) {
	ms := &mockEventStore{}
	h := NewHandler(ms, nil)
	r := buildIngestRouter("org-1", h)
	rec := doIngest(r, `{"id":"e1","subject":"u"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rec.Code)
	}
}

func TestIngestHandler_MissingSubject(t *testing.T) {
	ms := &mockEventStore{}
	h := NewHandler(ms, nil)
	r := buildIngestRouter("org-1", h)
	rec := doIngest(r, `{"id":"e1","type":"t"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rec.Code)
	}
}

func TestIngestHandler_InvalidJSON(t *testing.T) {
	ms := &mockEventStore{}
	h := NewHandler(ms, nil)
	r := buildIngestRouter("org-1", h)
	rec := doIngest(r, `not-json`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rec.Code)
	}
}

// ── Response shape ────────────────────────────────────────────────────────────

func TestIngestHandler_ResponseShape(t *testing.T) {
	ms := &mockEventStore{received: []int{2}, duplicates: []int{1}}
	h := NewHandler(ms, nil)
	r := buildIngestRouter("org-1", h)

	body := `[{"id":"r1","type":"t","subject":"u"},{"id":"r2","type":"t","subject":"u"}]`
	rec := doIngest(r, body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]int
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["received"]; !ok {
		t.Error("response missing 'received' field")
	}
	if _, ok := resp["duplicates"]; !ok {
		t.Error("response missing 'duplicates' field")
	}
	if resp["received"] != 2 || resp["duplicates"] != 1 {
		t.Errorf("want received=2 dup=1, got %v", resp)
	}
}

// ── Time passthrough ──────────────────────────────────────────────────────────

// When a time field is provided, it must be forwarded to the store as-is.
func TestIngestHandler_TimePassthrough(t *testing.T) {
	ms := &mockEventStore{}
	h := NewHandler(ms, nil)
	r := buildIngestRouter("org-1", h)

	rec := doIngest(r, `{"id":"t1","type":"x","subject":"s","time":"2020-01-01T00:00:00Z"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d: %s", rec.Code, rec.Body.String())
	}
	got := ms.calls[0][0].Time
	if got == nil {
		t.Fatal("time should be forwarded (not nil)")
	}
	if got.Year() != 2020 {
		t.Errorf("time year: want 2020, got %d", got.Year())
	}
}

// When time is omitted, the store receives nil (server assigns now()).
func TestIngestHandler_OmittedTime_IsNil(t *testing.T) {
	ms := &mockEventStore{}
	h := NewHandler(ms, nil)
	r := buildIngestRouter("org-1", h)

	rec := doIngest(r, `{"id":"t2","type":"x","subject":"s"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d: %s", rec.Code, rec.Body.String())
	}
	if ms.calls[0][0].Time != nil {
		t.Error("omitted time should be nil in store.IngestEvent")
	}
}
