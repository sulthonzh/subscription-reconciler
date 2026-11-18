package middleware

import (
	"net/http"
	"strings"
)

var allowedMethods = strings.Join([]string{
	http.MethodGet,
	http.MethodPost,
	http.MethodOptions,
}, ", ")

var allowedHeaders = strings.Join([]string{
	"Content-Type",
	"Authorization",
	"X-Webhook-Token",
}, ", ")

// ParseCORSAllowedOrigins turns a comma-separated environment value into an exact-origin allowlist.
func ParseCORSAllowedOrigins(value string) []string {
	origins := make([]string, 0)
	for _, origin := range strings.Split(value, ",") {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			origins = append(origins, origin)
		}
	}
	return origins
}

// CORS allows only explicitly listed origins. An empty allowlist emits no Access-Control-Allow-Origin header.
func CORS(allowedOrigins ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			allowed[origin] = struct{}{}
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			_, originAllowed := allowed[origin]

			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", allowedMethods)
			w.Header().Set("Access-Control-Allow-Headers", allowedHeaders)

			if r.Method == http.MethodOptions {
				if !originAllowed {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.WriteHeader(http.StatusNoContent)
				return
			}

			if originAllowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
			next.ServeHTTP(w, r)
		})
	}
}
