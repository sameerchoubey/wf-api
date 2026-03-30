package middleware

import (
	"context"
	"net/http"
	"strings"

	"wealthflow/backend/internal/auth"
	"wealthflow/backend/internal/respond"
)

type ctxKey string

const UserIDKey ctxKey = "user_id"

func BearerAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if h == "" || !strings.HasPrefix(strings.ToLower(h), "bearer ") {
				respond.Error(w, http.StatusUnauthorized, "Not authenticated")
				return
			}
			raw := strings.TrimSpace(h[7:])
			if raw == "" {
				respond.Error(w, http.StatusUnauthorized, "Not authenticated")
				return
			}
			uid, err := auth.ParseUserID(raw, secret)
			if err != nil {
				respond.Error(w, http.StatusUnauthorized, "Invalid token")
				return
			}
			ctx := context.WithValue(r.Context(), UserIDKey, uid)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func UserID(ctx context.Context) string {
	v, _ := ctx.Value(UserIDKey).(string)
	return v
}
