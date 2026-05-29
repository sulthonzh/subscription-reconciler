package middleware

import (
	"bytes"
	"io"
	"net/http"
)

const maxBodySize = 1 << 20 // 1 MB

// BodySizeLimit rejects requests whose body exceeds 1 MB.
func BodySizeLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			limited := io.LimitReader(r.Body, int64(maxBodySize)+1)
			data, err := io.ReadAll(limited)
			_ = r.Body.Close()
			if err != nil {
				http.Error(w, "failed to read request body", http.StatusBadRequest)
				return
			}
			if len(data) > maxBodySize {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(data))
		}
		next.ServeHTTP(w, r)
	})
}
