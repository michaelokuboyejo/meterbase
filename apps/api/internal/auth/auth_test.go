package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockResolver maps key hashes to org IDs.
type mockResolver map[string]string

func (m mockResolver) ResolveKey(_ context.Context, keyHash string) (string, error) {
	orgID, ok := m[keyHash]
	if !ok {
		return "", errors.New("key not found")
	}
	return orgID, nil
}

// captureOrgID is a handler that records the org ID it sees in context.
func captureOrgID(t *testing.T, got *string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := OrgIDFromContext(r.Context())
		if !ok {
			t.Error("OrgIDFromContext: not set in context")
			return
		}
		*got = id
		w.WriteHeader(http.StatusOK)
	}
}

// --- Key generation & hashing ---

func TestGenerateKey(t *testing.T) {
	raw, prefix, hash, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey error: %v", err)
	}
	if !strings.HasPrefix(raw, "mb_") {
		t.Errorf("rawKey should start with 'mb_', got %q", raw)
	}
	if !strings.HasPrefix(raw, prefix) {
		t.Errorf("prefix %q is not a prefix of rawKey %q", prefix, raw)
	}
	if len(prefix) != 12 {
		t.Errorf("prefix length should be 12, got %d", len(prefix))
	}
	if hash == raw {
		t.Error("hash should not equal rawKey")
	}
	if hash == "" {
		t.Error("hash should not be empty")
	}
}

func TestHashKey_deterministic(t *testing.T) {
	key := "mb_someTestKey"
	h1 := HashKey(key)
	h2 := HashKey(key)
	if h1 != h2 {
		t.Errorf("HashKey not deterministic: %q != %q", h1, h2)
	}
}

func TestHashKey_differentInputs(t *testing.T) {
	if HashKey("mb_keyA") == HashKey("mb_keyB") {
		t.Error("distinct keys should not produce the same hash")
	}
}

// --- Middleware: DoD test 1 — valid key resolves correct org ---

func TestMiddleware_validKey(t *testing.T) {
	rawKey := "mb_validTestKey"
	orgID := "org-a-uuid"
	resolver := mockResolver{HashKey(rawKey): orgID}

	var capturedOrgID string
	handler := Middleware(resolver)(captureOrgID(t, &capturedOrgID))

	req := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if capturedOrgID != orgID {
		t.Errorf("expected org %q in context, got %q", orgID, capturedOrgID)
	}
}

func TestMiddleware_validKey_xAPIKeyHeader(t *testing.T) {
	rawKey := "mb_xapiKey"
	orgID := "org-via-x-api-key"
	resolver := mockResolver{HashKey(rawKey): orgID}

	var capturedOrgID string
	handler := Middleware(resolver)(captureOrgID(t, &capturedOrgID))

	req := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
	req.Header.Set("X-API-Key", rawKey)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if capturedOrgID != orgID {
		t.Errorf("expected org %q in context, got %q", orgID, capturedOrgID)
	}
}

// --- Middleware: DoD test 2 — invalid/revoked key → 401 ---

func TestMiddleware_missingBearer(t *testing.T) {
	resolver := mockResolver{}
	handler := Middleware(resolver)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called without a valid key")
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestMiddleware_invalidKey(t *testing.T) {
	// resolver always returns error (key not found / revoked)
	resolver := mockResolver{}
	handler := Middleware(resolver)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called with an invalid key")
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
	req.Header.Set("Authorization", "Bearer mb_unknownKey")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestMiddleware_malformedAuthHeader(t *testing.T) {
	resolver := mockResolver{}
	handler := Middleware(resolver)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
	req.Header.Set("Authorization", "Token notBearer")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// --- Middleware: DoD test 3 — cross-org tenancy isolation ---

func TestMiddleware_tenancyIsolation(t *testing.T) {
	rawKeyA := "mb_keyForOrgA"
	rawKeyB := "mb_keyForOrgB"
	orgAID := "org-a-00000000"
	orgBID := "org-b-11111111"

	resolver := mockResolver{
		HashKey(rawKeyA): orgAID,
		HashKey(rawKeyB): orgBID,
	}

	captureAndAssert := func(rawKey, expectedOrgID, otherOrgID string) {
		t.Helper()
		var capturedOrgID string
		handler := Middleware(resolver)(captureOrgID(t, &capturedOrgID))

		req := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
		req.Header.Set("Authorization", "Bearer "+rawKey)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("key %q: expected 200, got %d", rawKey, rec.Code)
		}
		if capturedOrgID != expectedOrgID {
			t.Errorf("key %q: expected org %q, got %q", rawKey, expectedOrgID, capturedOrgID)
		}
		if capturedOrgID == otherOrgID {
			t.Errorf("key %q yielded the other org's ID — isolation violated", rawKey)
		}
	}

	// Key A must resolve to org A only
	captureAndAssert(rawKeyA, orgAID, orgBID)
	// Key B must resolve to org B only
	captureAndAssert(rawKeyB, orgBID, orgAID)
}
