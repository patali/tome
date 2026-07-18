package auth

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// Limiter is a small per-IP sliding-window rate limiter, used only on the
// invite-redemption endpoint to blunt code guessing.
type Limiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	limit  int
	window time.Duration
}

func NewLimiter(limit int, window time.Duration) *Limiter {
	return &Limiter{hits: make(map[string][]time.Time), limit: limit, window: window}
}

// Allow records an attempt from ip and reports whether it is within limits.
func (l *Limiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-l.window)
	kept := l.hits[ip][:0]
	for _, t := range l.hits[ip] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.limit {
		l.hits[ip] = kept
		return false
	}
	l.hits[ip] = append(kept, time.Now())
	// Opportunistic cleanup of idle IPs so the map can't grow unbounded.
	if len(l.hits) > 1024 {
		for k, v := range l.hits {
			if len(v) == 0 || !v[len(v)-1].After(cutoff) {
				delete(l.hits, k)
			}
		}
	}
	return true
}

// Wrap enforces the limit keyed by the connection's remote IP. Deliberately
// ignores X-Forwarded-For — Tome is designed to face clients directly.
func (l *Limiter) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		if !l.Allow(ip) {
			writeError(w, http.StatusTooManyRequests, "too many attempts, try again later")
			return
		}
		next.ServeHTTP(w, r)
	})
}
