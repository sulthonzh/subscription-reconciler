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
}, ", ")

// CORS sets permissive CORS headers on every response and handles OPTIONS preflight.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", allowedMethods)
		w.Header().Set("Access-Control-Allow-Headers", allowedHeaders)

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
