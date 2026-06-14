package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type mockPinger struct{ err error }

func (m *mockPinger) Ping(_ context.Context) error { return m.err }

func TestHealthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	handleHealthz(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	body := strings.TrimSpace(rec.Body.String())
	if body != `{"status":"ok"}` {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestReadyz_ready(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	handleReadyz(&mockPinger{err: nil}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	body := strings.TrimSpace(rec.Body.String())
	if body != `{"status":"ready"}` {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestReadyz_unavailable(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	handleReadyz(&mockPinger{err: errors.New("db down")}).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}
