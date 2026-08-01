package auth

import (
	"net"
	"net/http"
	"os"
	"strings"
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

// trustedIPHeader (TOME_TRUSTED_IP_HEADER, e.g. "CF-Connecting-IP") names a
// header carrying the real client IP. Unset by default: a forwarding header is
// trivially forged by anyone who can reach the server directly, and believing
// one would turn per-IP limits into no limits at all.
//
// Set it only when every request provably arrives through a proxy that
// overwrites the header. That is true of this project's own deployment — the
// stack publishes no ports and cloudflared is the sole ingress — and it matters
// there, because otherwise every visitor shares the tunnel's compose-network
// address and one caller's attempts exhaust everyone's budget.
var trustedIPHeader = strings.TrimSpace(os.Getenv("TOME_TRUSTED_IP_HEADER"))

// ClientIP is the address rate limits are keyed on.
func ClientIP(r *http.Request) string {
	if trustedIPHeader != "" {
		// Leftmost entry: chained proxies append, so the first is the origin.
		if v := r.Header.Get(trustedIPHeader); v != "" {
			if i := strings.Index(v, ","); i >= 0 {
				v = v[:i]
			}
			if v = strings.TrimSpace(v); v != "" {
				return v
			}
		}
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// Wrap enforces the limit keyed by the client IP.
func (l *Limiter) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := ClientIP(r)
		if !l.Allow(ip) {
			writeError(w, http.StatusTooManyRequests, "too many attempts, try again later")
			return
		}
		next.ServeHTTP(w, r)
	})
}
