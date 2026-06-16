package dashauth

import (
	"net/http"
	"strings"

	"github.com/mykelokuboyejo/meterbase/apps/api/internal/respond"
)

// DashMiddleware validates the dashboard session bearer token and injects the
// authenticated User into the request context.
func DashMiddleware(store *Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := bearerToken(r)
			if raw == "" {
				respond.Error(w, http.StatusUnauthorized, "missing_session", "Authentication required")
				return
			}
			user, err := store.ResolveSession(r.Context(), hashToken(raw))
			if err != nil {
				respond.Error(w, http.StatusUnauthorized, "invalid_session", "Session is invalid or expired")
				return
			}
			next.ServeHTTP(w, r.WithContext(withUser(r.Context(), user)))
		})
	}
}

func bearerToken(r *http.Request) string {
	v := r.Header.Get("Authorization")
	if t, ok := strings.CutPrefix(v, "Bearer "); ok {
		return t
	}
	return ""
}
