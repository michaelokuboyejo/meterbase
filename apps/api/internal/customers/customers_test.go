package customers

import (
	"bytes"
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

// mockKeyResolver resolves every key to a fixed orgID — used for handler unit tests.
type mockKeyResolver struct{ orgID string }

func (m *mockKeyResolver) ResolveKey(_ context.Context, _ string) (string, error) {
	return m.orgID, nil
}

// mockRepo is an in-memory stub that satisfies Repository.
type mockRepo struct {
	customers map[string]*store.Customer // keyed by id
	byExt     map[string]string          // orgID:externalID -> id
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		customers: make(map[string]*store.Customer),
		byExt:     make(map[string]string),
	}
}

func (m *mockRepo) CreateCustomer(_ context.Context, orgID, externalID string, name *string, metadata json.RawMessage) (*store.Customer, error) {
	key := orgID + ":" + externalID
	if _, dup := m.byExt[key]; dup {
		return nil, store.ErrDuplicateExternalID
	}
	if len(metadata) == 0 {
		metadata = json.RawMessage("{}")
	}
	c := &store.Customer{
		ID:         "cus-" + externalID,
		OrgID:      orgID,
		ExternalID: externalID,
		Name:       name,
		Metadata:   metadata,
		CreatedAt:  time.Now(),
	}
	m.customers[c.ID] = c
	m.byExt[key] = c.ID
	return c, nil
}

func (m *mockRepo) GetCustomer(_ context.Context, orgID, customerID string) (*store.Customer, error) {
	c, ok := m.customers[customerID]
	if !ok || c.OrgID != orgID {
		return nil, store.ErrCustomerNotFound
	}
	return c, nil
}

func (m *mockRepo) ListCustomers(_ context.Context, orgID string, _ int, _ string) ([]*store.Customer, string, error) {
	var out []*store.Customer
	for _, c := range m.customers {
		if c.OrgID == orgID {
			out = append(out, c)
		}
	}
	return out, "", nil
}

func (m *mockRepo) UpdateCustomer(_ context.Context, orgID, customerID string, upd store.CustomerUpdate) (*store.Customer, error) {
	c, ok := m.customers[customerID]
	if !ok || c.OrgID != orgID {
		return nil, store.ErrCustomerNotFound
	}
	if upd.SetName {
		c.Name = upd.Name
	}
	if upd.SetMetadata {
		c.Metadata = upd.Metadata
	}
	return c, nil
}

// buildRouter wires the customer handler behind the real auth middleware, with
// a mock resolver that always resolves to orgID.
func buildRouter(orgID string, h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(auth.Middleware(&mockKeyResolver{orgID: orgID}))
	r.Route("/v1/customers", h.Routes)
	return r
}

// do executes a request against r with a bearer token header already set.
func do(r http.Handler, method, path, body string) *httptest.ResponseRecorder {
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

func TestCreateCustomer_Success(t *testing.T) {
	h := NewHandler(newMockRepo())
	r := buildRouter("org-1", h)

	rec := do(r, http.MethodPost, "/v1/customers",
		`{"externalId":"cust-1","name":"Alice","metadata":{"tier":"gold"}}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["id"] == nil || resp["id"] == "" {
		t.Error("id missing from response")
	}
	if resp["externalId"] != "cust-1" {
		t.Errorf("externalId: want cust-1, got %v", resp["externalId"])
	}
	if resp["createdAt"] == nil {
		t.Error("createdAt missing from response")
	}
}

func TestCreateCustomer_MissingExternalID(t *testing.T) {
	h := NewHandler(newMockRepo())
	r := buildRouter("org-1", h)

	rec := do(r, http.MethodPost, "/v1/customers", `{"name":"Alice"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rec.Code)
	}
}

// DoD: duplicate externalId in the same org returns 409.
func TestCreateCustomer_DuplicateExternalID(t *testing.T) {
	h := NewHandler(newMockRepo())
	r := buildRouter("org-1", h)

	do(r, http.MethodPost, "/v1/customers", `{"externalId":"dup"}`)
	rec := do(r, http.MethodPost, "/v1/customers", `{"externalId":"dup"}`)
	if rec.Code != http.StatusConflict {
		t.Errorf("want 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetCustomer_Success(t *testing.T) {
	repo := newMockRepo()
	c, _ := repo.CreateCustomer(context.Background(), "org-1", "ext-g", nil, nil)

	h := NewHandler(repo)
	r := buildRouter("org-1", h)

	rec := do(r, http.MethodGet, "/v1/customers/"+c.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["id"] != c.ID {
		t.Errorf("id mismatch: want %s, got %v", c.ID, resp["id"])
	}
}

func TestGetCustomer_NotFound(t *testing.T) {
	h := NewHandler(newMockRepo())
	r := buildRouter("org-1", h)

	rec := do(r, http.MethodGet, "/v1/customers/nonexistent-id", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", rec.Code)
	}
}

// DoD: org A cannot access org B's customer.
func TestGetCustomer_CrossOrgIsolation(t *testing.T) {
	repo := newMockRepo()
	c, _ := repo.CreateCustomer(context.Background(), "org-A", "ext-iso", nil, nil)

	h := NewHandler(repo)
	r := buildRouter("org-B", h) // authenticated as org-B

	rec := do(r, http.MethodGet, "/v1/customers/"+c.ID, "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("cross-org get: want 404, got %d", rec.Code)
	}
}

func TestListCustomers_Success(t *testing.T) {
	repo := newMockRepo()
	if _, err := repo.CreateCustomer(context.Background(), "org-1", "ext-l1", nil, nil); err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}
	if _, err := repo.CreateCustomer(context.Background(), "org-1", "ext-l2", nil, nil); err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}

	h := NewHandler(repo)
	r := buildRouter("org-1", h)

	rec := do(r, http.MethodGet, "/v1/customers", "")
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
		t.Errorf("want 2 customers, got %d", len(resp.Data))
	}
}

func TestListCustomers_OrgIsolation(t *testing.T) {
	repo := newMockRepo()
	if _, err := repo.CreateCustomer(context.Background(), "org-1", "ext-mine", nil, nil); err != nil {
		t.Fatalf("CreateCustomer org-1: %v", err)
	}
	if _, err := repo.CreateCustomer(context.Background(), "org-2", "ext-theirs", nil, nil); err != nil {
		t.Fatalf("CreateCustomer org-2: %v", err)
	}

	h := NewHandler(repo)
	r := buildRouter("org-1", h)

	rec := do(r, http.MethodGet, "/v1/customers", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var resp struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Errorf("org isolation: want 1 customer for org-1, got %d", len(resp.Data))
	}
}

func TestPatchCustomer_Name(t *testing.T) {
	repo := newMockRepo()
	c, _ := repo.CreateCustomer(context.Background(), "org-1", "ext-p", ptrStr("Old"), nil)

	h := NewHandler(repo)
	r := buildRouter("org-1", h)

	body, _ := json.Marshal(map[string]any{"name": "New"})
	rec := do(r, http.MethodPatch, "/v1/customers/"+c.ID, string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["name"] != "New" {
		t.Errorf("name: want New, got %v", resp["name"])
	}
}

func TestPatchCustomer_NullName(t *testing.T) {
	repo := newMockRepo()
	c, _ := repo.CreateCustomer(context.Background(), "org-1", "ext-null", ptrStr("Old"), nil)

	h := NewHandler(repo)
	r := buildRouter("org-1", h)

	rec := do(r, http.MethodPatch, "/v1/customers/"+c.ID, `{"name":null}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["name"] != nil {
		t.Errorf("name: want null, got %v", resp["name"])
	}
}

// DoD: PATCH with only metadata must leave name unchanged.
func TestPatchCustomer_AbsentFieldsUnchanged(t *testing.T) {
	repo := newMockRepo()
	c, _ := repo.CreateCustomer(context.Background(), "org-1", "ext-absent", ptrStr("Keep Me"), json.RawMessage(`{"k":"v"}`))

	h := NewHandler(repo)
	r := buildRouter("org-1", h)

	rec := do(r, http.MethodPatch, "/v1/customers/"+c.ID, `{"metadata":{"x":"y"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["name"] != "Keep Me" {
		t.Errorf("absent field: name should be unchanged, got %v", resp["name"])
	}
}

func TestPatchCustomer_NotFound(t *testing.T) {
	h := NewHandler(newMockRepo())
	r := buildRouter("org-1", h)

	rec := do(r, http.MethodPatch, "/v1/customers/no-such-id", `{"name":"X"}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", rec.Code)
	}
}

// DoD: org B cannot modify org A's customer.
func TestPatchCustomer_CrossOrgIsolation(t *testing.T) {
	repo := newMockRepo()
	c, _ := repo.CreateCustomer(context.Background(), "org-A", "ext-cross-patch", nil, nil)

	h := NewHandler(repo)
	r := buildRouter("org-B", h)

	rec := do(r, http.MethodPatch, "/v1/customers/"+c.ID, `{"name":"hijacked"}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("cross-org patch: want 404, got %d", rec.Code)
	}
}

func TestCreateCustomer_InvalidJSON(t *testing.T) {
	h := NewHandler(newMockRepo())
	r := buildRouter("org-1", h)

	req := httptest.NewRequest(http.MethodPost, "/v1/customers", bytes.NewBufferString("not-json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rec.Code)
	}
}

// Verify error response shape matches the contract: {"error":{"code":"...","message":"..."}}
func TestErrorResponseShape(t *testing.T) {
	h := NewHandler(newMockRepo())
	r := buildRouter("org-1", h)

	rec := do(r, http.MethodGet, "/v1/customers/missing-id", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rec.Code)
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Error.Code == "" {
		t.Error("error.code should not be empty")
	}
	if body.Error.Message == "" {
		t.Error("error.message should not be empty")
	}
}

func ptrStr(s string) *string { return &s }
