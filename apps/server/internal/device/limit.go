package device

import (
	"net"
	"net/http"
	"sync"
	"time"
)

type attempt struct {
	start time.Time
	count int
}
type Limiter struct {
	mu      sync.Mutex
	sources map[string]attempt
}

func (l *Limiter) Allow(source string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.sources == nil {
		l.sources = map[string]attempt{}
	}
	for key, a := range l.sources {
		if now.Sub(a.start) >= time.Minute {
			delete(l.sources, key)
		}
	}
	a := l.sources[source]
	if a.count >= 20 || (a.count == 0 && len(l.sources) >= 1024) {
		return false
	}
	if a.count == 0 {
		a.start = now
	}
	a.count++
	l.sources[source] = a
	return true
}

func Caller(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return "route:device"
	}
	ip := net.ParseIP(host)
	// No PROXY listener is enabled; tunnels share the device route's budget.
	if ip == nil || ip.IsLoopback() {
		return "route:device"
	}
	return ip.String()
}

func Reject(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write(make([]byte, 64))
}
