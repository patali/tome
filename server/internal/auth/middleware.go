package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/patali/tome/server/internal/store"
)

type ctxKey struct{}

// UserFrom returns the authenticated user stashed by RequireUser.
func UserFrom(ctx context.Context) *store.User {
	u, _ := ctx.Value(ctxKey{}).(*store.User)
	return u
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	writeError(w, http.StatusUnauthorized, "unauthorized")
}

// RequireUser authenticates the Bearer API key and stashes the user in the
// request context. Bad key, unknown key, and disabled account all yield the
// same 401 (no oracle).
func RequireUser(st *store.Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		key = strings.TrimSpace(key)
		if !ok || !strings.HasPrefix(key, "tome_") {
			unauthorized(w)
			return
		}
		u, err := st.UserByKeyHash(HashKey(key))
		if err != nil || u.Disabled {
			unauthorized(w)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	})
}

// RequireAdmin is RequireUser plus an is_admin check.
func RequireAdmin(st *store.Store, next http.Handler) http.Handler {
	return RequireUser(st, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !UserFrom(r.Context()).IsAdmin {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(w, r)
	}))
}
