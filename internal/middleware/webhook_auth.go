package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
)

// WebhookAuth requires a shared operator token before a mutating webhook reaches its handler.
// An empty configured token fails closed; cmd/server is responsible for refusing startup in production.
func WebhookAuth(configuredToken string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			presented := strings.TrimSpace(r.Header.Get("X-Webhook-Token"))
			if presented == "" {
				presented = strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
			}

			configuredHash := sha256.Sum256([]byte(configuredToken))
			presentedHash := sha256.Sum256([]byte(presented))
			if configuredToken == "" || subtle.ConstantTimeCompare(configuredHash[:], presentedHash[:]) != 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
