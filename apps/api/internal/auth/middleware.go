package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// KeyResolver maps a hashed API key to an org ID.
// The store.OrgStore implements this interface.
type KeyResolver interface {
	ResolveKey(ctx context.Context, keyHash string) (orgID string, err error)
}

// Middleware authenticates requests via bearer token or X-API-Key header.
// On success it places the resolved org ID in the request context via withOrgID.
// On failure it returns 401 and does not call next.
func Middleware(resolver KeyResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := bearerToken(r)
			if raw == "" {
				writeUnauthorized(w, "missing_api_key", "API key required")
				return
			}
			orgID, err := resolver.ResolveKey(r.Context(), HashKey(raw))
			if err != nil {
				writeUnauthorized(w, "invalid_api_key", "Invalid or revoked API key")
				return
			}
			next.ServeHTTP(w, r.WithContext(withOrgID(r.Context(), orgID)))
		})
	}
}

// bearerToken extracts the raw API key from the request.
// It accepts "Authorization: Bearer <key>" and "X-API-Key: <key>" headers.
func bearerToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if after, ok := strings.CutPrefix(h, "Bearer "); ok {
			return strings.TrimSpace(after)
		}
	}
	return r.Header.Get("X-API-Key")
}

func writeUnauthorized(w http.ResponseWriter, code, message string) {
	type errDetail struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	type errBody struct {
		Error errDetail `json:"error"`
	}
	body := errBody{Error: errDetail{Code: code, Message: message}}
	data, _ := json.Marshal(body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write(data)
}
