// Package e2e contains end-to-end tests that spin up the real HTTP server
// against a live PostgreSQL/TimescaleDB instance and exercise the API as a
// user would: making HTTP requests with real API keys and verifying status
// codes and response bodies. Tests are skipped when DATABASE_URL is unset.
package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/api"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/auth"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/store"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/usage"
)

// ── Test infrastructure ───────────────────────────────────────────────────────

type testEnv struct {
	url  string // base URL, e.g. http://127.0.0.1:PORT
	pool *pgxpool.Pool
	orgs *store.OrgStore
}

type tenant struct {
	key   string // raw API key (shown once, never stored in DB)
	orgID string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping E2E test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := store.NewPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to DB: %v", err)
	}
	t.Cleanup(pool.Close)

	orgStore := store.NewOrgStore(pool)
	customerStore := store.NewCustomerStore(pool)
	meterStore := store.NewMeterStore(pool)
	eventStore := store.NewTimescaleEventStore(pool)
	usageRepo := usage.NewTimescaleUsageRepository(pool)
	planStore := store.NewPlanStore(pool)

	handler := api.NewRouter(pool, orgStore, customerStore, meterStore, eventStore, eventStore, usageRepo, planStore)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return &testEnv{url: srv.URL, pool: pool, orgs: orgStore}
}

// newTenant creates a fresh org and API key for this test.
// The raw key is returned for use in Authorization headers.
func (e *testEnv) newTenant(t *testing.T, label string) tenant {
	t.Helper()
	name := fmt.Sprintf("e2e-%s-%d", label, time.Now().UnixNano())
	org, err := e.orgs.CreateOrg(context.Background(), name)
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
	raw, prefix, hash, err := auth.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if _, err := e.orgs.CreateAPIKey(context.Background(), org.ID, "e2e-key", hash, prefix); err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	return tenant{key: raw, orgID: org.ID}
}

// call sends a JSON HTTP request, optionally decodes the response body into out,
// and returns the HTTP status code. The body is always fully consumed and closed.
func (e *testEnv) call(t *testing.T, method, path, key string, reqBody, out any) int {
	t.Helper()
	var body io.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, e.url+path, body)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, path, err)
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode %s %s response (status %d): %v", method, path, resp.StatusCode, err)
		}
	} else {
		_, _ = io.Copy(io.Discard, resp.Body)
	}
	return resp.StatusCode
}

// callRaw sends a request and returns the response with an open body.
// The caller is responsible for closing resp.Body.
func (e *testEnv) callRaw(t *testing.T, method, path, key string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, e.url+path, nil)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, path, err)
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	return resp
}

// uid generates a unique string safe to use in slugs and event IDs.
func uid(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// timeWindow returns RFC3339 from/to strings covering ±n minutes around now.
func timeWindow(n int) (from, to string) {
	now := time.Now().UTC()
	return now.Add(-time.Duration(n) * time.Minute).Format(time.RFC3339),
		now.Add(time.Duration(n) * time.Minute).Format(time.RFC3339)
}

// ── Health checks ─────────────────────────────────────────────────────────────

// TestE2E_Health confirms both health endpoints respond 200 without an API key.
func TestE2E_Health(t *testing.T) {
	e := newTestEnv(t)
	for _, path := range []string{"/healthz", "/readyz"} {
		if code := e.call(t, "GET", path, "", nil, nil); code != 200 {
			t.Errorf("%s: want 200, got %d", path, code)
		}
	}
}

// ── Authentication ────────────────────────────────────────────────────────────

// TestE2E_Auth_NoKey verifies that omitting the Authorization header returns 401.
func TestE2E_Auth_NoKey(t *testing.T) {
	e := newTestEnv(t)
	if code := e.call(t, "GET", "/v1/customers", "", nil, nil); code != 401 {
		t.Errorf("no key: want 401, got %d", code)
	}
}

// TestE2E_Auth_InvalidKey verifies that an unknown key returns 401.
func TestE2E_Auth_InvalidKey(t *testing.T) {
	e := newTestEnv(t)
	// A syntactically valid mb_ key that is not in the database.
	fakeKey := "mb_" + strings.Repeat("0", 64)
	if code := e.call(t, "GET", "/v1/meters", fakeKey, nil, nil); code != 401 {
		t.Errorf("unknown key: want 401, got %d", code)
	}
}

// TestE2E_Auth_RevokedKey creates a key, confirms it works, revokes it,
// then confirms subsequent requests return 401.
func TestE2E_Auth_RevokedKey(t *testing.T) {
	e := newTestEnv(t)
	ten := e.newTenant(t, "revoke")

	// The key must work before revocation.
	var listResp map[string]any
	if code := e.call(t, "GET", "/v1/customers", ten.key, nil, &listResp); code != 200 {
		t.Fatalf("before revoke: want 200, got %d", code)
	}

	// Look up the key ID in the DB via its hash, then revoke it.
	keyHash := auth.HashKey(ten.key)
	var keyID string
	if err := e.pool.QueryRow(context.Background(),
		`SELECT id FROM api_keys WHERE key_hash=$1 AND revoked_at IS NULL`,
		keyHash,
	).Scan(&keyID); err != nil {
		t.Fatalf("find key ID: %v", err)
	}
	if err := e.orgs.RevokeAPIKey(context.Background(), ten.orgID, keyID); err != nil {
		t.Fatalf("revoke key: %v", err)
	}

	// Now the same key must return 401.
	if code := e.call(t, "GET", "/v1/customers", ten.key, nil, nil); code != 401 {
		t.Errorf("after revoke: want 401, got %d", code)
	}
}

// ── Event ingestion ───────────────────────────────────────────────────────────

// TestE2E_Ingest_Single posts a single event and verifies 202 with received=1, duplicates=0.
func TestE2E_Ingest_Single(t *testing.T) {
	e := newTestEnv(t)
	ten := e.newTenant(t, "ingest-single")

	var resp map[string]int
	code := e.call(t, "POST", "/v1/events", ten.key, map[string]any{
		"id":      uid("evt"),
		"type":    "api_request",
		"subject": "user-1",
		"data":    map[string]any{"tokens": 100},
	}, &resp)

	if code != 202 {
		t.Fatalf("want 202, got %d", code)
	}
	if resp["received"] != 1 || resp["duplicates"] != 0 {
		t.Errorf("want received=1 duplicates=0, got received=%d duplicates=%d",
			resp["received"], resp["duplicates"])
	}
}

// TestE2E_Ingest_Dedup posts the same event ID twice and confirms it is stored only once.
// This is the core billing-correctness guarantee.
func TestE2E_Ingest_Dedup(t *testing.T) {
	e := newTestEnv(t)
	ten := e.newTenant(t, "dedup")

	evtID := uid("dedup-evt")
	evt := map[string]any{"id": evtID, "type": "api_request", "subject": "user-1"}

	var r1, r2 map[string]int

	if code := e.call(t, "POST", "/v1/events", ten.key, evt, &r1); code != 202 {
		t.Fatalf("first ingest: want 202, got %d", code)
	}
	if r1["received"] != 1 || r1["duplicates"] != 0 {
		t.Errorf("first: want received=1 dup=0, got received=%d dup=%d",
			r1["received"], r1["duplicates"])
	}

	if code := e.call(t, "POST", "/v1/events", ten.key, evt, &r2); code != 202 {
		t.Fatalf("second ingest: want 202, got %d", code)
	}
	if r2["received"] != 0 || r2["duplicates"] != 1 {
		t.Errorf("second: want received=0 dup=1, got received=%d dup=%d",
			r2["received"], r2["duplicates"])
	}
}

// TestE2E_Ingest_BatchWithDuplicate posts a 3-element batch where one element
// is a duplicate and confirms the response correctly splits received vs. duplicates.
func TestE2E_Ingest_BatchWithDuplicate(t *testing.T) {
	e := newTestEnv(t)
	ten := e.newTenant(t, "batch-dup")

	idA := uid("batch-a")
	idB := uid("batch-b")

	batch := []map[string]any{
		{"id": idA, "type": "t", "subject": "s"},
		{"id": idB, "type": "t", "subject": "s"},
		{"id": idA, "type": "t", "subject": "s"}, // duplicate of first
	}

	var resp map[string]int
	if code := e.call(t, "POST", "/v1/events", ten.key, batch, &resp); code != 202 {
		t.Fatalf("want 202, got %d", code)
	}
	if resp["received"] != 2 || resp["duplicates"] != 1 {
		t.Errorf("want received=2 dup=1, got received=%d dup=%d",
			resp["received"], resp["duplicates"])
	}
}

// TestE2E_Ingest_LateEvent posts an event with a time 7 days in the past and
// confirms it is stored with the caller-supplied event time, not the ingest time.
func TestE2E_Ingest_LateEvent(t *testing.T) {
	e := newTestEnv(t)
	ten := e.newTenant(t, "late-evt")

	lateTime := time.Now().UTC().Add(-7 * 24 * time.Hour).Truncate(time.Second)
	evtID := uid("late")

	var ingestResp map[string]int
	if code := e.call(t, "POST", "/v1/events", ten.key, map[string]any{
		"id":      evtID,
		"type":    "api_request",
		"subject": "user-late",
		"time":    lateTime.Format(time.RFC3339),
	}, &ingestResp); code != 202 {
		t.Fatalf("ingest: want 202, got %d", code)
	}
	if ingestResp["received"] != 1 {
		t.Fatalf("received: want 1, got %d", ingestResp["received"])
	}

	// Retrieve the event within a window around its event time.
	fromStr := lateTime.Add(-time.Hour).Format(time.RFC3339)
	toStr := lateTime.Add(time.Hour).Format(time.RFC3339)

	var listResp struct {
		Data []struct {
			ID   string `json:"id"`
			Time string `json:"time"`
		} `json:"data"`
	}
	if code := e.call(t, "GET",
		fmt.Sprintf("/v1/events?from=%s&to=%s&type=api_request", fromStr, toStr),
		ten.key, nil, &listResp); code != 200 {
		t.Fatalf("list: want 200, got %d", code)
	}

	found := false
	for _, ev := range listResp.Data {
		if ev.ID != evtID {
			continue
		}
		found = true
		parsed, err := time.Parse(time.RFC3339, ev.Time)
		if err != nil {
			t.Fatalf("parse event time %q: %v", ev.Time, err)
		}
		if parsed.Sub(lateTime).Abs() > time.Second {
			t.Errorf("event time: want %v, got %v", lateTime, parsed)
		}
	}
	if !found {
		t.Errorf("late event %s not found in listing over its event-time window", evtID)
	}
}

// TestE2E_Ingest_UnknownType confirms that an event whose type matches no meter
// is still accepted (store-and-flag policy).
func TestE2E_Ingest_UnknownType(t *testing.T) {
	e := newTestEnv(t)
	ten := e.newTenant(t, "unknown-type")

	var resp map[string]int
	if code := e.call(t, "POST", "/v1/events", ten.key, map[string]any{
		"id":      uid("unk"),
		"type":    "no_meter_for_this_type_at_all",
		"subject": "user-1",
	}, &resp); code != 202 {
		t.Fatalf("want 202, got %d", code)
	}
	if resp["received"] != 1 {
		t.Errorf("want received=1, got %d", resp["received"])
	}
}

// TestE2E_Ingest_MissingRequiredFields confirms that events missing id, type,
// or subject return 400.
func TestE2E_Ingest_MissingRequiredFields(t *testing.T) {
	e := newTestEnv(t)
	ten := e.newTenant(t, "bad-event")

	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing id", map[string]any{"type": "t", "subject": "s"}},
		{"missing type", map[string]any{"id": uid("x"), "subject": "s"}},
		{"missing subject", map[string]any{"id": uid("x"), "type": "t"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if code := e.call(t, "POST", "/v1/events", ten.key, tc.body, nil); code != 400 {
				t.Errorf("%s: want 400, got %d", tc.name, code)
			}
		})
	}
}

// ── Customers ─────────────────────────────────────────────────────────────────

// TestE2E_Customers_CRUD exercises the full create → get → list → patch lifecycle.
func TestE2E_Customers_CRUD(t *testing.T) {
	e := newTestEnv(t)
	ten := e.newTenant(t, "customers-crud")

	// Create
	var created struct {
		ID         string  `json:"id"`
		ExternalID string  `json:"externalId"`
		Name       *string `json:"name"`
	}
	if code := e.call(t, "POST", "/v1/customers", ten.key, map[string]any{
		"externalId": "cust-ext-001",
		"name":       "Alice",
	}, &created); code != 201 {
		t.Fatalf("create: want 201, got %d", code)
	}
	if created.ID == "" {
		t.Fatal("create: id must not be empty")
	}
	if created.ExternalID != "cust-ext-001" {
		t.Errorf("externalId: want cust-ext-001, got %s", created.ExternalID)
	}
	if created.Name == nil || *created.Name != "Alice" {
		t.Errorf("name: want Alice, got %v", created.Name)
	}

	// Get by ID
	var got struct {
		ID         string `json:"id"`
		ExternalID string `json:"externalId"`
	}
	if code := e.call(t, "GET", "/v1/customers/"+created.ID, ten.key, nil, &got); code != 200 {
		t.Fatalf("get: want 200, got %d", code)
	}
	if got.ID != created.ID {
		t.Errorf("get: ID mismatch, want %s got %s", created.ID, got.ID)
	}

	// List — the new customer must be present.
	var list struct {
		Data []struct{ ID string `json:"id"` } `json:"data"`
	}
	if code := e.call(t, "GET", "/v1/customers", ten.key, nil, &list); code != 200 {
		t.Fatalf("list: want 200, got %d", code)
	}
	foundInList := false
	for _, c := range list.Data {
		if c.ID == created.ID {
			foundInList = true
		}
	}
	if !foundInList {
		t.Errorf("list: created customer %s not in response", created.ID)
	}

	// Patch — update the name.
	var patched struct {
		Name *string `json:"name"`
	}
	if code := e.call(t, "PATCH", "/v1/customers/"+created.ID, ten.key, map[string]any{
		"name": "Alice Updated",
	}, &patched); code != 200 {
		t.Fatalf("patch: want 200, got %d", code)
	}
	if patched.Name == nil || *patched.Name != "Alice Updated" {
		t.Errorf("patch name: want Alice Updated, got %v", patched.Name)
	}
}

// TestE2E_Customers_DuplicateExternalID confirms that creating two customers with
// the same externalId in the same org returns 409.
func TestE2E_Customers_DuplicateExternalID(t *testing.T) {
	e := newTestEnv(t)
	ten := e.newTenant(t, "cust-dup")

	body := map[string]any{"externalId": "same-ext-id"}
	if code := e.call(t, "POST", "/v1/customers", ten.key, body, nil); code != 201 {
		t.Fatalf("first create: want 201, got %d", code)
	}
	if code := e.call(t, "POST", "/v1/customers", ten.key, body, nil); code != 409 {
		t.Errorf("duplicate externalId: want 409, got %d", code)
	}
}

// TestE2E_Customers_CrossOrgIsolation confirms that org B cannot read org A's customers.
func TestE2E_Customers_CrossOrgIsolation(t *testing.T) {
	e := newTestEnv(t)
	tenA := e.newTenant(t, "cust-iso-a")
	tenB := e.newTenant(t, "cust-iso-b")

	var custA struct{ ID string `json:"id"` }
	if code := e.call(t, "POST", "/v1/customers", tenA.key, map[string]any{
		"externalId": "cross-org-user",
	}, &custA); code != 201 {
		t.Fatalf("create customer A: want 201, got %d", code)
	}

	// Org B GET of org A's customer ID must be 404.
	if code := e.call(t, "GET", "/v1/customers/"+custA.ID, tenB.key, nil, nil); code != 404 {
		t.Errorf("cross-org GET customer: want 404, got %d", code)
	}

	// Org B's list must not include org A's customer.
	var listB struct {
		Data []struct{ ID string `json:"id"` } `json:"data"`
	}
	if code := e.call(t, "GET", "/v1/customers", tenB.key, nil, &listB); code != 200 {
		t.Fatalf("list B: want 200, got %d", code)
	}
	for _, c := range listB.Data {
		if c.ID == custA.ID {
			t.Errorf("cross-org leak: org B can see org A's customer %s", custA.ID)
		}
	}
}

// ── Meters ────────────────────────────────────────────────────────────────────

// TestE2E_Meters_CRUD exercises create → get → list → delete.
func TestE2E_Meters_CRUD(t *testing.T) {
	e := newTestEnv(t)
	ten := e.newTenant(t, "meters-crud")

	slug := uid("api-requests")

	// Create
	var created struct {
		ID          string `json:"id"`
		Slug        string `json:"slug"`
		EventType   string `json:"eventType"`
		Aggregation string `json:"aggregation"`
	}
	if code := e.call(t, "POST", "/v1/meters", ten.key, map[string]any{
		"slug":        slug,
		"eventType":   "api_request",
		"aggregation": "COUNT",
	}, &created); code != 201 {
		t.Fatalf("create: want 201, got %d", code)
	}
	if created.Slug != slug {
		t.Errorf("slug: want %s, got %s", slug, created.Slug)
	}
	if created.Aggregation != "COUNT" {
		t.Errorf("aggregation: want COUNT, got %s", created.Aggregation)
	}

	// Get
	var got struct{ Slug string `json:"slug"` }
	if code := e.call(t, "GET", "/v1/meters/"+slug, ten.key, nil, &got); code != 200 {
		t.Fatalf("get: want 200, got %d", code)
	}
	if got.Slug != slug {
		t.Errorf("get slug: want %s, got %s", slug, got.Slug)
	}

	// List — meter must appear.
	var list struct {
		Data []struct{ Slug string `json:"slug"` } `json:"data"`
	}
	if code := e.call(t, "GET", "/v1/meters", ten.key, nil, &list); code != 200 {
		t.Fatalf("list: want 200, got %d", code)
	}
	foundInList := false
	for _, m := range list.Data {
		if m.Slug == slug {
			foundInList = true
		}
	}
	if !foundInList {
		t.Errorf("list: meter %s not in response", slug)
	}

	// Delete
	if code := e.call(t, "DELETE", "/v1/meters/"+slug, ten.key, nil, nil); code != 204 {
		t.Errorf("delete: want 204, got %d", code)
	}

	// Get after delete → 404.
	if code := e.call(t, "GET", "/v1/meters/"+slug, ten.key, nil, nil); code != 404 {
		t.Errorf("get after delete: want 404, got %d", code)
	}
}

// TestE2E_Meters_DuplicateSlug confirms that creating two meters with the same slug
// in the same org returns 409.
func TestE2E_Meters_DuplicateSlug(t *testing.T) {
	e := newTestEnv(t)
	ten := e.newTenant(t, "meters-dup")

	slug := uid("dup-meter")
	body := map[string]any{"slug": slug, "eventType": "t", "aggregation": "COUNT"}

	if code := e.call(t, "POST", "/v1/meters", ten.key, body, nil); code != 201 {
		t.Fatalf("first create: want 201, got %d", code)
	}
	if code := e.call(t, "POST", "/v1/meters", ten.key, body, nil); code != 409 {
		t.Errorf("duplicate slug: want 409, got %d", code)
	}
}

// TestE2E_Meters_QueryAfterIngest creates a COUNT meter, ingests 3 events, then
// queries with windowSize=MINUTE (which reads from the raw hypertable immediately,
// no continuous-aggregate refresh required).
func TestE2E_Meters_QueryAfterIngest(t *testing.T) {
	e := newTestEnv(t)
	ten := e.newTenant(t, "meter-query")

	slug := uid("req-count")

	if code := e.call(t, "POST", "/v1/meters", ten.key, map[string]any{
		"slug":        slug,
		"eventType":   "api_req",
		"aggregation": "COUNT",
	}, nil); code != 201 {
		t.Fatalf("create meter: want 201, got %d", code)
	}

	for i := range 3 {
		if code := e.call(t, "POST", "/v1/events", ten.key, map[string]any{
			"id":      fmt.Sprintf("qevt-%d-%d", i, time.Now().UnixNano()),
			"type":    "api_req",
			"subject": "user-query",
		}, nil); code != 202 {
			t.Fatalf("ingest %d: want 202, got %d", i, code)
		}
	}

	from, to := timeWindow(5)
	var result struct {
		Meter      string `json:"meter"`
		WindowSize string `json:"windowSize"`
		Data       []struct {
			Bucket string  `json:"bucket"`
			Value  float64 `json:"value"`
		} `json:"data"`
	}
	if code := e.call(t, "GET",
		fmt.Sprintf("/v1/meters/%s/query?from=%s&to=%s&windowSize=MINUTE", slug, from, to),
		ten.key, nil, &result); code != 200 {
		t.Fatalf("query: want 200, got %d", code)
	}

	if result.Meter != slug {
		t.Errorf("meter label: want %s, got %s", slug, result.Meter)
	}
	if result.WindowSize != "MINUTE" {
		t.Errorf("windowSize: want MINUTE, got %s", result.WindowSize)
	}

	var total float64
	for _, dp := range result.Data {
		total += dp.Value
	}
	if total != 3 {
		t.Errorf("total COUNT: want 3, got %v (data: %+v)", total, result.Data)
	}
}

// TestE2E_Meters_QueryInvalidParams confirms that missing or invalid query parameters
// return 400.
func TestE2E_Meters_QueryInvalidParams(t *testing.T) {
	e := newTestEnv(t)
	ten := e.newTenant(t, "meter-bad-query")

	slug := uid("bad-q-meter")
	if code := e.call(t, "POST", "/v1/meters", ten.key, map[string]any{
		"slug": slug, "eventType": "t", "aggregation": "COUNT",
	}, nil); code != 201 {
		t.Fatalf("create meter: want 201, got %d", code)
	}

	from, to := timeWindow(5)

	// Missing windowSize.
	if code := e.call(t, "GET",
		fmt.Sprintf("/v1/meters/%s/query?from=%s&to=%s", slug, from, to),
		ten.key, nil, nil); code != 400 {
		t.Errorf("missing windowSize: want 400, got %d", code)
	}

	// Unsupported windowSize value.
	if code := e.call(t, "GET",
		fmt.Sprintf("/v1/meters/%s/query?from=%s&to=%s&windowSize=WEEK", slug, from, to),
		ten.key, nil, nil); code != 400 {
		t.Errorf("invalid windowSize: want 400, got %d", code)
	}
}

// ── Events list & export ──────────────────────────────────────────────────────

// TestE2E_Events_List ingests events then retrieves them via GET /v1/events
// with subject and type filters.
func TestE2E_Events_List(t *testing.T) {
	e := newTestEnv(t)
	ten := e.newTenant(t, "events-list")

	subject := uid("list-user")
	evtType := "list_event"

	for i := range 3 {
		if code := e.call(t, "POST", "/v1/events", ten.key, map[string]any{
			"id":      fmt.Sprintf("list-evt-%d-%d", i, time.Now().UnixNano()),
			"type":    evtType,
			"subject": subject,
		}, nil); code != 202 {
			t.Fatalf("ingest %d: want 202, got %d", i, code)
		}
	}

	from, to := timeWindow(5)
	var resp struct {
		Data []struct {
			ID      string `json:"id"`
			Subject string `json:"subject"`
		} `json:"data"`
	}
	path := fmt.Sprintf("/v1/events?from=%s&to=%s&subject=%s&type=%s", from, to, subject, evtType)
	if code := e.call(t, "GET", path, ten.key, nil, &resp); code != 200 {
		t.Fatalf("list: want 200, got %d", code)
	}
	if len(resp.Data) != 3 {
		t.Errorf("want 3 events, got %d", len(resp.Data))
	}
	for _, ev := range resp.Data {
		if ev.Subject != subject {
			t.Errorf("event subject mismatch: want %s, got %s", subject, ev.Subject)
		}
	}
}

// TestE2E_Events_Export_JSON ingests an event then exports it as JSON.
func TestE2E_Events_Export_JSON(t *testing.T) {
	e := newTestEnv(t)
	ten := e.newTenant(t, "export-json")

	evtID := uid("export-json-evt")
	if code := e.call(t, "POST", "/v1/events", ten.key, map[string]any{
		"id":      evtID,
		"type":    "export_test",
		"subject": "user-export",
	}, nil); code != 202 {
		t.Fatalf("ingest: want 202, got %d", code)
	}

	from, to := timeWindow(5)
	resp := e.callRaw(t, "GET",
		fmt.Sprintf("/v1/events/export?format=json&from=%s&to=%s&type=export_test", from, to),
		ten.key)
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("export JSON: want 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type: want application/json, got %q", ct)
	}

	var events []struct{ ID string `json:"id"` }
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		t.Fatalf("decode JSON export: %v", err)
	}
	found := false
	for _, ev := range events {
		if ev.ID == evtID {
			found = true
		}
	}
	if !found {
		t.Errorf("exported event %s not found in JSON export", evtID)
	}
}

// TestE2E_Events_Export_CSV ingests an event then exports it as CSV,
// verifying the header row and that the event ID appears in the body.
func TestE2E_Events_Export_CSV(t *testing.T) {
	e := newTestEnv(t)
	ten := e.newTenant(t, "export-csv")

	evtID := uid("csv-evt")
	if code := e.call(t, "POST", "/v1/events", ten.key, map[string]any{
		"id":      evtID,
		"type":    "csv_test",
		"subject": "user-csv",
	}, nil); code != 202 {
		t.Fatalf("ingest: want 202, got %d", code)
	}

	from, to := timeWindow(5)
	resp := e.callRaw(t, "GET",
		fmt.Sprintf("/v1/events/export?format=csv&from=%s&to=%s&type=csv_test", from, to),
		ten.key)
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("export CSV: want 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Errorf("Content-Type: want text/csv, got %q", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	csv := string(body)

	lines := strings.SplitN(csv, "\n", 2)
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "id,type,source,subject") {
		t.Errorf("CSV header: want id,type,..., got %q", lines[0])
	}
	if !strings.Contains(csv, evtID) {
		t.Errorf("CSV body: event ID %s not found", evtID)
	}
}

// ── Pricing ───────────────────────────────────────────────────────────────────

// TestE2E_Pricing_PAYG exercises the full PAYG cost-computation user flow:
// customer → SUM meter → ingest events → plan → rate card → compute cost.
// Also asserts reproducibility: computing the same period twice yields the same total.
func TestE2E_Pricing_PAYG(t *testing.T) {
	e := newTestEnv(t)
	ten := e.newTenant(t, "pricing-payg")

	// Create a customer whose externalId will be used as the event subject.
	var cust struct{ ID string `json:"id"` }
	if code := e.call(t, "POST", "/v1/customers", ten.key, map[string]any{
		"externalId": "payg-user",
	}, &cust); code != 201 {
		t.Fatalf("create customer: want 201, got %d", code)
	}

	// Create SUM meter on the "tokens" field.
	meterSlug := uid("payg-tokens")
	var meter struct{ ID string `json:"id"` }
	if code := e.call(t, "POST", "/v1/meters", ten.key, map[string]any{
		"slug":          meterSlug,
		"eventType":     "token_usage",
		"aggregation":   "SUM",
		"valueProperty": "tokens",
	}, &meter); code != 201 {
		t.Fatalf("create meter: want 201, got %d", code)
	}

	// Ingest 3 events totalling 600 tokens; subject matches customer externalId.
	for i, tokens := range []int{100, 200, 300} {
		if code := e.call(t, "POST", "/v1/events", ten.key, map[string]any{
			"id":      fmt.Sprintf("payg-evt-%d-%d", i, time.Now().UnixNano()),
			"type":    "token_usage",
			"subject": "payg-user",
			"data":    map[string]any{"tokens": tokens},
		}, nil); code != 202 {
			t.Fatalf("ingest[%d]: want 202, got %d", i, code)
		}
	}

	// Create plan.
	var plan struct{ ID string `json:"id"` }
	if code := e.call(t, "POST", "/v1/plans", ten.key, map[string]any{
		"name": "PAYG Plan",
	}, &plan); code != 201 {
		t.Fatalf("create plan: want 201, got %d", code)
	}

	// Attach PAYG rate card: $0.01 per token.
	if code := e.call(t, "POST", "/v1/plans/"+plan.ID+"/rate-cards", ten.key, map[string]any{
		"meterId":  meter.ID,
		"model":    "PAYG",
		"currency": "USD",
		"config":   map[string]any{"unitPrice": 0.01},
	}, nil); code != 201 {
		t.Fatalf("create rate card: want 201, got %d", code)
	}

	from, to := timeWindow(10)
	costPath := fmt.Sprintf("/v1/plans/%s/cost?customerId=%s&from=%s&to=%s",
		plan.ID, cust.ID, from, to)

	var costResp struct {
		Currency  string  `json:"currency"`
		Total     float64 `json:"total"`
		LineItems []struct {
			Meter string  `json:"meter"`
			Usage float64 `json:"usage"`
			Cost  float64 `json:"cost"`
		} `json:"lineItems"`
	}
	if code := e.call(t, "GET", costPath, ten.key, nil, &costResp); code != 200 {
		t.Fatalf("compute cost: want 200, got %d", code)
	}

	if costResp.Currency != "USD" {
		t.Errorf("currency: want USD, got %s", costResp.Currency)
	}
	// 100 + 200 + 300 = 600 tokens × $0.01 = $6.00
	if costResp.Total != 6.0 {
		t.Errorf("total: want 6.00, got %v", costResp.Total)
	}
	if len(costResp.LineItems) != 1 {
		t.Fatalf("lineItems: want 1, got %d", len(costResp.LineItems))
	}
	if costResp.LineItems[0].Usage != 600 {
		t.Errorf("usage: want 600, got %v", costResp.LineItems[0].Usage)
	}
	if costResp.LineItems[0].Cost != 6.0 {
		t.Errorf("line cost: want 6.00, got %v", costResp.LineItems[0].Cost)
	}

	// Reproducibility: recomputing must yield an identical total.
	var costResp2 struct{ Total float64 `json:"total"` }
	if code := e.call(t, "GET", costPath, ten.key, nil, &costResp2); code != 200 {
		t.Fatalf("recompute: want 200, got %d", code)
	}
	if costResp2.Total != costResp.Total {
		t.Errorf("reproducibility: first=%.4f second=%.4f", costResp.Total, costResp2.Total)
	}
}

// ── Tenancy isolation ─────────────────────────────────────────────────────────

// TestE2E_TenancyIsolation ingests events under org A and confirms they are
// invisible to org B on every read surface: event list, meter query, and export.
func TestE2E_TenancyIsolation(t *testing.T) {
	e := newTestEnv(t)
	tenA := e.newTenant(t, "iso-a")
	tenB := e.newTenant(t, "iso-b")

	evtType := uid("iso-type")
	slug := uid("iso-meter")

	// Org A: create meter and ingest 2 events.
	if code := e.call(t, "POST", "/v1/meters", tenA.key, map[string]any{
		"slug":        slug,
		"eventType":   evtType,
		"aggregation": "COUNT",
	}, nil); code != 201 {
		t.Fatalf("create meter for A: want 201, got %d", code)
	}
	for i := range 2 {
		if code := e.call(t, "POST", "/v1/events", tenA.key, map[string]any{
			"id":      fmt.Sprintf("iso-evt-a-%d-%d", i, time.Now().UnixNano()),
			"type":    evtType,
			"subject": "subject-a",
		}, nil); code != 202 {
			t.Fatalf("ingest A[%d]: want 202, got %d", i, code)
		}
	}

	from, to := timeWindow(5)

	// Org B event list must return zero results for org A's event type.
	var listB struct {
		Data []struct{ ID string `json:"id"` } `json:"data"`
	}
	if code := e.call(t, "GET",
		fmt.Sprintf("/v1/events?from=%s&to=%s&type=%s", from, to, evtType),
		tenB.key, nil, &listB); code != 200 {
		t.Fatalf("list B: want 200, got %d", code)
	}
	if len(listB.Data) != 0 {
		t.Errorf("isolation: org B sees %d of org A's events", len(listB.Data))
	}

	// Org B cannot access org A's meter by slug.
	if code := e.call(t, "GET", "/v1/meters/"+slug, tenB.key, nil, nil); code != 404 {
		t.Errorf("isolation: org B can access org A's meter, want 404 got %d", code)
	}
}

// ── Developer integration flow (PRD Flow A — the 10-minute path) ──────────────

// TestE2E_DeveloperFlow simulates the canonical first-use path from the PRD:
// create customer → define meter → ingest events → query usage → verify the numbers.
// This is the single most important correctness test: if it passes, the product works.
func TestE2E_DeveloperFlow(t *testing.T) {
	e := newTestEnv(t)
	ten := e.newTenant(t, "flow-a")

	// Step 1 — Create a customer.
	var cust struct {
		ID         string `json:"id"`
		ExternalID string `json:"externalId"`
	}
	if code := e.call(t, "POST", "/v1/customers", ten.key, map[string]any{
		"externalId": "developer-flow-user",
		"name":       "Developer",
	}, &cust); code != 201 {
		t.Fatalf("step 1 (create customer): want 201, got %d", code)
	}
	if cust.ID == "" {
		t.Fatal("step 1: customer ID must not be empty")
	}

	// Step 2 — Define a SUM meter on the "tokens" field.
	slug := uid("token-meter")
	var meter struct {
		ID          string `json:"id"`
		Slug        string `json:"slug"`
		Aggregation string `json:"aggregation"`
	}
	if code := e.call(t, "POST", "/v1/meters", ten.key, map[string]any{
		"slug":          slug,
		"eventType":     "token_usage",
		"aggregation":   "SUM",
		"valueProperty": "tokens",
	}, &meter); code != 201 {
		t.Fatalf("step 2 (create meter): want 201, got %d", code)
	}
	if meter.Aggregation != "SUM" {
		t.Errorf("step 2: aggregation: want SUM, got %s", meter.Aggregation)
	}

	// Step 3 — Ingest two events under the customer's externalId as subject.
	// Total tokens: 250 + 750 = 1000.
	for i, tokens := range []int{250, 750} {
		if code := e.call(t, "POST", "/v1/events", ten.key, map[string]any{
			"id":      fmt.Sprintf("flow-evt-%d-%d", i, time.Now().UnixNano()),
			"type":    "token_usage",
			"subject": cust.ExternalID,
			"data":    map[string]any{"tokens": tokens},
		}, nil); code != 202 {
			t.Fatalf("step 3 (ingest[%d]): want 202, got %d", i, code)
		}
	}

	// Step 4 — Query usage with MINUTE window (reads raw hypertable, no rollup wait).
	from, to := timeWindow(5)
	var queryResult struct {
		Meter      string `json:"meter"`
		WindowSize string `json:"windowSize"`
		Data       []struct {
			Bucket string  `json:"bucket"`
			Value  float64 `json:"value"`
		} `json:"data"`
	}
	if code := e.call(t, "GET",
		fmt.Sprintf("/v1/meters/%s/query?from=%s&to=%s&windowSize=MINUTE&subject=%s",
			slug, from, to, cust.ExternalID),
		ten.key, nil, &queryResult); code != 200 {
		t.Fatalf("step 4 (query): want 200, got %d", code)
	}
	if queryResult.Meter != slug {
		t.Errorf("step 4: meter label: want %s, got %s", slug, queryResult.Meter)
	}

	var totalTokens float64
	for _, dp := range queryResult.Data {
		totalTokens += dp.Value
	}
	if totalTokens != 1000 {
		t.Errorf("step 4: total tokens: want 1000, got %v (buckets: %+v)",
			totalTokens, queryResult.Data)
	}

	// Step 5 — Confirm the raw events are retrievable for audit purposes.
	var listResp struct {
		Data []struct{ ID string `json:"id"` } `json:"data"`
	}
	if code := e.call(t, "GET",
		fmt.Sprintf("/v1/events?from=%s&to=%s&subject=%s&type=token_usage",
			from, to, cust.ExternalID),
		ten.key, nil, &listResp); code != 200 {
		t.Fatalf("step 5 (list events): want 200, got %d", code)
	}
	if len(listResp.Data) != 2 {
		t.Errorf("step 5: want 2 raw events, got %d", len(listResp.Data))
	}
}
