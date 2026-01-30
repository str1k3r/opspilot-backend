package auth

import (
	"context"
	"net/http"
)

type contextKey string

const userIDKey contextKey = "opspilot_user_id"

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(cookieName)
		if err != nil || cookie.Value == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		claims, err := ParseToken(cookie.Value)
		if err != nil || claims.Subject == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), userIDKey, claims.Subject)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func UserIDFromContext(ctx context.Context) (string, bool) {
	value := ctx.Value(userIDKey)
	userID, ok := value.(string)
	return userID, ok
}
