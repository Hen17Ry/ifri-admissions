package drafts

import (
	"net"
	"net/http"
	"sync"
	"time"
)

type window struct {
	until time.Time
	count int
}
type createLimiter struct {
	mu      sync.Mutex
	entries map[string]window
}

func (l *createLimiter) allow(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for ip, value := range l.entries {
		if now.After(value.until) {
			delete(l.entries, ip)
		}
	}
	value := l.entries[host]
	if value.until.IsZero() {
		if len(l.entries) >= 1024 {
			return false
		}
		value.until = now.Add(time.Minute)
	}
	if value.count >= 5 {
		return false
	}
	value.count++
	l.entries[host] = value
	return true
}
