package webhooks_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/store"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/webhooks"
)

func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := store.NewPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("getTestPool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("mustMarshal: %v", err)
	}
	return json.RawMessage(b)
}

// TestComputeSignature — DoD test 2: SIGNATURE
// Verifies that ComputeSignature returns "sha256=<hex(HMAC-SHA256(secret, body))>".
func TestComputeSignature(t *testing.T) {
	secret := []byte("my-test-secret")
	body := []byte(`{"type":"alert.triggered","value":1050000}`)

	got := webhooks.ComputeSignature(secret, body)

	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if got != want {
		t.Errorf("ComputeSignature: got %q, want %q", got, want)
	}
}

// TestDispatcher_SignatureOnDelivery verifies that the dispatcher includes the correct
// X-MeterBase-Signature header on outbound webhook requests.
func TestDispatcher_SignatureOnDelivery(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	secret := "signature-test-secret"
	var receivedSig string
	var receivedBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSig = r.Header.Get("X-MeterBase-Signature")
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	orgStore := store.NewOrgStore(pool)
	alertStore := store.NewAlertStore(pool)

	org, _ := orgStore.CreateOrg(ctx, "sig-test-"+t.Name())
	ep, _ := alertStore.CreateWebhookEndpoint(ctx, org.ID, srv.URL, secret)
	payload := mustMarshal(t, map[string]any{"type": "alert.triggered"})
	if _, err := alertStore.CreateDelivery(ctx, ep.ID, "alert.triggered", payload); err != nil {
		t.Fatalf("CreateDelivery: %v", err)
	}

	d := webhooks.NewDispatcher(alertStore, &http.Client{Timeout: 5 * time.Second}, 5)
	d.Dispatch(ctx)

	expectedSig := webhooks.ComputeSignature([]byte(secret), receivedBody)
	if receivedSig != expectedSig {
		t.Errorf("signature mismatch: got %q, want %q", receivedSig, expectedSig)
	}
}

// TestDispatcher_RetryAndCap — DoD test 3: RETRY
// Verifies that a non-2xx response:
//   - increments attempts on each Dispatch call
//   - eventually sets status to 'failed' when attempts reach maxAttempts
func TestDispatcher_RetryAndCap(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	// Server always returns 503.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	orgStore := store.NewOrgStore(pool)
	alertStore := store.NewAlertStore(pool)

	org, _ := orgStore.CreateOrg(ctx, "retry-test-"+t.Name())
	ep, _ := alertStore.CreateWebhookEndpoint(ctx, org.ID, srv.URL, "retrySecret")
	payload := mustMarshal(t, map[string]any{"type": "alert.triggered"})
	del, _ := alertStore.CreateDelivery(ctx, ep.ID, "alert.triggered", payload)

	const maxAttempts = 3
	d := webhooks.NewDispatcher(alertStore, &http.Client{Timeout: 5 * time.Second}, maxAttempts)

	// Drive maxAttempts dispatch cycles. Between each, reset next_attempt to NULL so
	// the delivery is claimable immediately (bypassing the exponential backoff delay).
	for i := 0; i < maxAttempts; i++ {
		if _, err := pool.Exec(ctx,
			"UPDATE webhook_deliveries SET next_attempt = NULL WHERE id = $1::uuid",
			del.ID,
		); err != nil {
			t.Fatalf("reset next_attempt[%d]: %v", i, err)
		}
		d.Dispatch(ctx)
	}

	// After maxAttempts failures the delivery must be 'failed'.
	var status string
	var attempts int
	if err := pool.QueryRow(ctx,
		"SELECT status, attempts FROM webhook_deliveries WHERE id = $1::uuid",
		del.ID,
	).Scan(&status, &attempts); err != nil {
		t.Fatalf("query delivery: %v", err)
	}
	if status != "failed" {
		t.Errorf("expected status=failed after %d attempts, got %q", maxAttempts, status)
	}
	if attempts != maxAttempts {
		t.Errorf("expected attempts=%d, got %d", maxAttempts, attempts)
	}
}

// TestDispatcher_SuccessOnFirstAttempt verifies that a 2xx response sets status='succeeded'.
func TestDispatcher_SuccessOnFirstAttempt(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	orgStore := store.NewOrgStore(pool)
	alertStore := store.NewAlertStore(pool)

	org, _ := orgStore.CreateOrg(ctx, "success-test-"+t.Name())
	ep, _ := alertStore.CreateWebhookEndpoint(ctx, org.ID, srv.URL, "ok-secret")
	payload := mustMarshal(t, map[string]any{"type": "alert.triggered"})
	del, _ := alertStore.CreateDelivery(ctx, ep.ID, "alert.triggered", payload)

	d := webhooks.NewDispatcher(alertStore, &http.Client{Timeout: 5 * time.Second}, 5)
	d.Dispatch(ctx)

	var status string
	var attempts int
	if err := pool.QueryRow(ctx,
		"SELECT status, attempts FROM webhook_deliveries WHERE id = $1::uuid",
		del.ID,
	).Scan(&status, &attempts); err != nil {
		t.Fatalf("query delivery: %v", err)
	}
	if status != "succeeded" {
		t.Errorf("expected status=succeeded, got %q", status)
	}
	if attempts != 1 {
		t.Errorf("expected attempts=1, got %d", attempts)
	}
}
