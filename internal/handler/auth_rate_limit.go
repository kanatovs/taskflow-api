package handler

import (
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"
)

const (
	authAttemptsPerWindow = 10
	authWindowDuration    = time.Minute
	authMaxClients        = 4096
)

type authWindow struct {
	attempts int
	resetAt  time.Time
}

type authRateLimiter struct {
	mutex      sync.Mutex
	clients    map[netip.Addr]authWindow
	nextExpiry time.Time
	now        func() time.Time
}

func newAuthRateLimiter() *authRateLimiter {
	return &authRateLimiter{clients: make(map[netip.Addr]authWindow), now: time.Now}
}

func (l *authRateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed, retryAfter := l.allow(authClientIP(r.RemoteAddr), l.now())
		if !allowed {
			seconds := max(1, int((retryAfter+time.Second-1)/time.Second))
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			w.Header().Set("Cache-Control", "no-store")
			writeError(w, r, http.StatusTooManyRequests, "rate_limited", "Too many authentication attempts", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (l *authRateLimiter) allow(ip netip.Addr, now time.Time) (bool, time.Duration) {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	window, known := l.clients[ip]
	if known && now.Before(window.resetAt) {
		if window.attempts >= authAttemptsPerWindow {
			return false, window.resetAt.Sub(now)
		}
		window.attempts++
		l.clients[ip] = window
		return true, 0
	}

	if !known && len(l.clients) >= authMaxClients {
		// Sweep only after an expiry, so rejected new clients cannot force a scan.
		if !now.Before(l.nextExpiry) {
			l.removeExpired(now)
		}
		if len(l.clients) >= authMaxClients {
			// Preserve active budgets instead of evicting them and allowing a bypass.
			return false, l.nextExpiry.Sub(now)
		}
	}

	window = authWindow{attempts: 1, resetAt: now.Add(authWindowDuration)}
	l.clients[ip] = window
	if l.nextExpiry.IsZero() || window.resetAt.Before(l.nextExpiry) {
		l.nextExpiry = window.resetAt
	}
	return true, 0
}

func (l *authRateLimiter) removeExpired(now time.Time) {
	l.nextExpiry = time.Time{}
	for ip, window := range l.clients {
		if !now.Before(window.resetAt) {
			delete(l.clients, ip)
		} else if l.nextExpiry.IsZero() || window.resetAt.Before(l.nextExpiry) {
			l.nextExpiry = window.resetAt
		}
	}
}

func authClientIP(remoteAddr string) netip.Addr {
	if peer, err := netip.ParseAddrPort(remoteAddr); err == nil {
		return peer.Addr().Unmap().WithZone("")
	}
	if ip, err := netip.ParseAddr(remoteAddr); err == nil {
		return ip.Unmap().WithZone("")
	}
	// Malformed peer addresses share one budget instead of bypassing the limit.
	return netip.Addr{}
}
