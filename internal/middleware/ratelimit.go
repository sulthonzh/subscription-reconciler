package middleware

import (
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	rateLimitWindow = time.Minute
	rateLimitMax    = 100
	cleanupInterval = 2 * time.Minute
)

type ipEntry struct {
	mu       sync.Mutex
	count    int
	resetAt  time.Time
}

// RateLimiter returns middleware that limits each IP to rateLimitMax requests per minute.
// A background goroutine periodically cleans stale entries.
func RateLimiter() func(http.Handler) http.Handler {
	var store sync.Map

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case now := <-ticker.C:
				cutoff := now.Add(-rateLimitWindow)
				store.Range(func(key, value interface{}) bool {
					entry := value.(*ipEntry)
					entry.mu.Lock()
					stale := entry.resetAt.Before(cutoff)
					entry.mu.Unlock()
					if stale {
						store.Delete(key)
					}
					return true
				})
			}
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}

			val, _ := store.LoadOrStore(ip, &ipEntry{
				resetAt: time.Now().Add(rateLimitWindow),
			})
			entry := val.(*ipEntry)

			entry.mu.Lock()
			if time.Now().After(entry.resetAt) {
				entry.count = 0
				entry.resetAt = time.Now().Add(rateLimitWindow)
			}
			entry.count++
			count := entry.count
			entry.mu.Unlock()

			if count > rateLimitMax {
				http.Error(w, "too many requests", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
