package middleware

import (
	"net"
	"net/http"
	"sync"
	"time"
)

type rateLimiterConfig struct {
	window          time.Duration
	max             int
	cleanupInterval time.Duration
}

type ipEntry struct {
	mu      sync.Mutex
	count   int
	resetAt time.Time
}

func newRateLimiter(cfg rateLimiterConfig) (func(http.Handler) http.Handler, func()) {
	var store sync.Map

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(cfg.cleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case now := <-ticker.C:
				cutoff := now.Add(-cfg.window)
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

	stop := func() { close(done) }

	handler := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}

			val, _ := store.LoadOrStore(ip, &ipEntry{
				resetAt: time.Now().Add(cfg.window),
			})
			entry := val.(*ipEntry)

			entry.mu.Lock()
			if time.Now().After(entry.resetAt) {
				entry.count = 0
				entry.resetAt = time.Now().Add(cfg.window)
			}
			entry.count++
			count := entry.count
			entry.mu.Unlock()

			if count > cfg.max {
				http.Error(w, "too many requests", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}

	return handler, stop
}

func RateLimiter() func(http.Handler) http.Handler {
	mw, _ := newRateLimiter(rateLimiterConfig{
		window:          time.Minute,
		max:             100,
		cleanupInterval: 2 * time.Minute,
	})
	return mw
}
