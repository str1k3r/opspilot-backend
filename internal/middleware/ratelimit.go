package middleware

import (
	"net"
	"net/http"
	"strings"
	"time"

	"opspilot-backend/internal/cache"
)

const (
	loginLimit        = 5
	loginWindow       = time.Minute
	enrollIPLimit     = 10
	enrollIPWindow    = time.Minute
	enrollTokenLimit  = 60
	enrollTokenWindow = time.Hour
	tokenPrefixLen    = 12
)

func RateLimitLogin(cacheClient cache.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			key := "rl:login:" + ip
			count, err := cacheClient.IncrWithTTL(key, loginWindow)
			if err == nil && count > loginLimit {
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func RateLimitEnrollIP(cacheClient cache.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			key := "rl:enroll:ip:" + ip
			count, err := cacheClient.IncrWithTTL(key, enrollIPWindow)
			if err == nil && count > enrollIPLimit {
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func RateLimitEnrollToken(cacheClient cache.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := strings.TrimSpace(r.Header.Get("X-Bootstrap-Token"))
			if token == "" {
				token = strings.TrimSpace(r.URL.Query().Get("token"))
			}
			if token != "" {
				prefix := token
				if len(prefix) > tokenPrefixLen {
					prefix = prefix[:tokenPrefixLen]
				}
				key := "rl:enroll:token:" + prefix
				count, err := cacheClient.IncrWithTTL(key, enrollTokenWindow)
				if err == nil && count > enrollTokenLimit {
					http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
