package security

import (
	"net"
	"net/http"
	"sync"
	"time"
)

type attempts struct {
	count   int
	expires time.Time
}

// AttemptLimiter has bounded memory even if every request uses a new identity.
type AttemptLimiter struct {
	mu      sync.Mutex
	entries map[string]attempts
	limit   int
	window  time.Duration
}

func NewAttemptLimiter(limit int, window time.Duration) *AttemptLimiter {
	return &AttemptLimiter{entries: make(map[string]attempts), limit: limit, window: window}
}

func (l *AttemptLimiter) Allow(key string) bool { return l.allowAt(key, time.Now()) }

func (l *AttemptLimiter) allowAt(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.entries[key]
	if !ok || !now.Before(a.expires) {
		if len(l.entries) >= 4096 {
			for key, old := range l.entries {
				if !now.Before(old.expires) {
					delete(l.entries, key)
				}
			}
			if _, exists := l.entries[key]; !exists && len(l.entries) >= 4096 {
				return false
			}
		}
		a = attempts{expires: now.Add(l.window)}
	}
	if a.count >= l.limit {
		return false
	}
	a.count++
	l.entries[key] = a
	return true
}

// Use the actual peer rather than trusting arbitrary X-Forwarded-For headers.
// Behind a proxy this is intentionally a shared secondary limit; account limits
// still protect password verification across distributed sources.
func PeerKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
