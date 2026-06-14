package meters

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/auth"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/store"
)

// mockKeyResolver resolves every key to a fixed orgID.
type mockKeyResolver struct{ orgID string }

func (m *mockKeyResolver) ResolveKey(_ context.Context, _ string) (string, error) {
	return m.orgID, nil
}

// mockMeterRepo is an in-memory stub satisfying Repository.
type mockMeterRepo struct {
	meters map[string]*store.Meter // keyed by slug
}

func newMockMeterRepo() *mockMeterRepo {
	return &mockMeterRepo{meters: make(map[string]*store.Meter)}
}

func (m *mockMeterRepo) CreateMeter(_ context.Context, orgID, slug, eventType, agg string, valueProp *string, groupBy []string) (*store.Meter, error) {
	key := orgID + ":" + slug
	if _, dup := m.meters[key]; dup {
		return nil, store.ErrDuplicateMeterSlug
	}
	if groupBy == nil {
		groupBy = []string{}
	}
	mt := &store.Meter{
		ID:            "meter-" + slug,
		OrgID:         orgID,
		Slug:          slug,
		EventType:     eventType,
		Aggregation:   agg,
		ValueProperty: valueProp,
		GroupBy:       groupBy,
		CreatedAt:     time.Now(),
	}
	m.meters[key] = mt
	return mt, nil
}

func (m *mockMeterRepo) GetMeterBySlug(_ context.Context, orgID, slug string) (*store.Meter, error) {
	key := orgID + ":" + slug
	mt, ok := m.meters[key]
	if !ok {
		return nil, store.ErrMeterNotFound
	}
	return mt, nil
}

func (m *mockMeterRepo) GetMeterByID(_ context.Context, orgID, meterID string) (*store.Meter, error) {
	for _, mt := range m.meters {
		if mt.OrgID == orgID && mt.ID == meterID {
			return mt, nil
		}
	}
	return nil, store.ErrMeterNotFound
}

func (m *mockMeterRepo) ListMeters(_ context.Context, orgID string, _ int, _ string) ([]*store.Meter, string, error) {
	var out []*store.Meter
	for _, mt := range m.meters {
		if mt.OrgID == orgID {
			out = append(out, mt)
		}
	}
	return out, "", nil
}

func (m *mockMeterRepo) DeleteMeter(_ context.Context, orgID, slug string) error {
	key := orgID + ":" + slug
	if _, ok := m.meters[key]; !ok {
		return store.ErrMeterNotFound
	}
	delete(m.meters, key)
	return nil
}

func buildMeterRouter(orgID string, h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(auth.Middleware(&mockKeyResolver{orgID: orgID}))
	r.Route("/v1/meters", h.Routes)
	return r
}

func doMeter(r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	var bodyReader *strings.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	} else {
		bodyReader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestCreateMeter_Success(t *testing.T) {
	h := NewHandler(newMockMeterRepo(), nil)
	r := buildMeterRouter("org-1", h)

	body := `{"slug":"api_req","eventType":"api_request","aggregation":"SUM","valueProperty":"tokens","groupBy":["model"]}`
	rec := doMeter(r, http.MethodPost, "/v1/meters", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["slug"] != "api_req" {
		t.Errorf("slug: want api_req, got %v", resp["slug"])
	}
	if resp["eventType"] != "api_request" {
		t.Errorf("eventType: want api_request, got %v", resp["eventType"])
	}
	if resp["aggregation"] != "SUM" {
		t.Errorf("aggregation: want SUM, got %v", resp["aggregation"])
	}
	if resp["id"] == nil {
		t.Error("id should not be nil")
	}
	if resp["createdAt"] == nil {
		t.Error("createdAt should not be nil")
	}
	groupBy, _ := resp["groupBy"].([]any)
	if len(groupBy) != 1 || groupBy[0] != "model" {
		t.Errorf("groupBy: want [model], got %v", resp["groupBy"])
	}
}

func TestCreateMeter_MissingSlug(t *testing.T) {
	h := NewHandler(newMockMeterRepo(), nil)
	r := buildMeterRouter("org-1", h)
	rec := doMeter(r, http.MethodPost, "/v1/meters", `{"eventType":"t","aggregation":"COUNT"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rec.Code)
	}
}

func TestCreateMeter_InvalidAggregation(t *testing.T) {
	h := NewHandler(newMockMeterRepo(), nil)
	r := buildMeterRouter("org-1", h)
	rec := doMeter(r, http.MethodPost, "/v1/meters", `{"slug":"s","eventType":"t","aggregation":"INVALID"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rec.Code)
	}
}

func TestCreateMeter_DuplicateSlug(t *testing.T) {
	h := NewHandler(newMockMeterRepo(), nil)
	r := buildMeterRouter("org-1", h)
	body := `{"slug":"dup","eventType":"t","aggregation":"COUNT"}`
	doMeter(r, http.MethodPost, "/v1/meters", body)
	rec := doMeter(r, http.MethodPost, "/v1/meters", body)
	if rec.Code != http.StatusConflict {
		t.Errorf("want 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetMeter_Success(t *testing.T) {
	repo := newMockMeterRepo()
	if _, err := repo.CreateMeter(context.Background(), "org-1", "tok", "api_req", "SUM", nil, nil); err != nil {
		t.Fatalf("setup: %v", err)
	}

	h := NewHandler(repo, nil)
	r := buildMeterRouter("org-1", h)
	rec := doMeter(r, http.MethodGet, "/v1/meters/tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["slug"] != "tok" {
		t.Errorf("slug: want tok, got %v", resp["slug"])
	}
}

func TestGetMeter_NotFound(t *testing.T) {
	h := NewHandler(newMockMeterRepo(), nil)
	r := buildMeterRouter("org-1", h)
	rec := doMeter(r, http.MethodGet, "/v1/meters/ghost", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", rec.Code)
	}
}

func TestGetMeter_CrossOrgIsolation(t *testing.T) {
	repo := newMockMeterRepo()
	if _, err := repo.CreateMeter(context.Background(), "org-A", "iso-slug", "t", "COUNT", nil, nil); err != nil {
		t.Fatalf("setup: %v", err)
	}

	h := NewHandler(repo, nil)
	r := buildMeterRouter("org-B", h) // authenticated as org-B
	rec := doMeter(r, http.MethodGet, "/v1/meters/iso-slug", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("cross-org get: want 404, got %d", rec.Code)
	}
}

func TestListMeters_Success(t *testing.T) {
	repo := newMockMeterRepo()
	if _, err := repo.CreateMeter(context.Background(), "org-1", "m1", "t", "COUNT", nil, nil); err != nil {
		t.Fatalf("setup m1: %v", err)
	}
	if _, err := repo.CreateMeter(context.Background(), "org-1", "m2", "t", "SUM", nil, nil); err != nil {
		t.Fatalf("setup m2: %v", err)
	}

	h := NewHandler(repo, nil)
	r := buildMeterRouter("org-1", h)
	rec := doMeter(r, http.MethodGet, "/v1/meters", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Errorf("want 2 meters, got %d", len(resp.Data))
	}
}

func TestDeleteMeter_Success(t *testing.T) {
	repo := newMockMeterRepo()
	if _, err := repo.CreateMeter(context.Background(), "org-1", "to-del", "t", "COUNT", nil, nil); err != nil {
		t.Fatalf("setup: %v", err)
	}

	h := NewHandler(repo, nil)
	r := buildMeterRouter("org-1", h)
	rec := doMeter(r, http.MethodDelete, "/v1/meters/to-del", "")
	if rec.Code != http.StatusNoContent {
		t.Errorf("want 204, got %d", rec.Code)
	}
}

func TestDeleteMeter_NotFound(t *testing.T) {
	h := NewHandler(newMockMeterRepo(), nil)
	r := buildMeterRouter("org-1", h)
	rec := doMeter(r, http.MethodDelete, "/v1/meters/ghost", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", rec.Code)
	}
}

// Response shape: groupBy must be [] not null when empty.
func TestMeterResponse_GroupByNeverNull(t *testing.T) {
	h := NewHandler(newMockMeterRepo(), nil)
	r := buildMeterRouter("org-1", h)

	rec := doMeter(r, http.MethodPost, "/v1/meters", `{"slug":"no-gb","eventType":"t","aggregation":"COUNT"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["groupBy"] == nil {
		t.Error("groupBy should be [] not null")
	}
}
