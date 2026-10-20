package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kanatovs/taskflow-api/internal/domain"
	"github.com/kanatovs/taskflow-api/internal/service"
)

func TestAuthRateLimitWindow(t *testing.T) {
	limiter := newAuthRateLimiter()
	ip := netip.MustParseAddr("192.0.2.1")
	started := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	for range authAttemptsPerWindow {
		if allowed, _ := limiter.allow(ip, started); !allowed {
			t.Fatal("request within budget rejected")
		}
	}
	for _, elapsed := range []time.Duration{0, 30 * time.Second, time.Minute - time.Nanosecond} {
		allowed, retryAfter := limiter.allow(ip, started.Add(elapsed))
		if allowed || retryAfter != time.Minute-elapsed {
			t.Fatalf("elapsed=%s allowed=%v retryAfter=%s", elapsed, allowed, retryAfter)
		}
	}
	// Rejections must not move the reset time, including at the exact boundary.
	if allowed, _ := limiter.allow(ip, started.Add(time.Minute)); !allowed {
		t.Fatal("window did not reset at its boundary")
	}
	if allowed, _ := limiter.allow(netip.MustParseAddr("192.0.2.2"), started); !allowed {
		t.Fatal("another IP inherited the exhausted budget")
	}
}

func TestAuthRateLimitConcurrentAttempts(t *testing.T) {
	limiter := newAuthRateLimiter()
	ip := netip.MustParseAddr("192.0.2.1")
	now := time.Now()
	var allowed atomic.Int64
	var workers sync.WaitGroup
	start := make(chan struct{})
	for range 100 {
		workers.Go(func() {
			<-start
			if ok, _ := limiter.allow(ip, now); ok {
				allowed.Add(1)
			}
		})
	}
	close(start)
	workers.Wait()
	if got := allowed.Load(); got != authAttemptsPerWindow {
		t.Fatalf("allowed %d concurrent attempts, want %d", got, authAttemptsPerWindow)
	}
}

func TestAuthRateLimitCapacity(t *testing.T) {
	limiter := newAuthRateLimiter()
	started := time.Now()
	first := netip.AddrFrom4([4]byte{198, 18, 0, 0})
	for i := range authMaxClients {
		ip := netip.AddrFrom4([4]byte{198, 18, byte(i >> 8), byte(i)})
		if allowed, _ := limiter.allow(ip, started); !allowed {
			t.Fatal("client rejected before table reached capacity")
		}
	}
	for range authAttemptsPerWindow - 1 {
		if allowed, _ := limiter.allow(first, started); !allowed {
			t.Fatal("known client lost its remaining budget at capacity")
		}
	}
	newIP := netip.MustParseAddr("203.0.113.1")
	if allowed, retryAfter := limiter.allow(newIP, started.Add(time.Second)); allowed || retryAfter != 59*time.Second {
		t.Fatalf("full table allowed=%v retryAfter=%s", allowed, retryAfter)
	}
	if len(limiter.clients) != authMaxClients {
		t.Fatal("table exceeded its bound or evicted an active client")
	}
	if allowed, _ := limiter.allow(first, started.Add(time.Second)); allowed {
		t.Fatal("new client reset an exhausted existing budget")
	}
	// Reset one known client first: the cached earliest expiry is now conservative.
	if allowed, _ := limiter.allow(first, started.Add(time.Minute)); !allowed {
		t.Fatal("known client's expired budget did not reset")
	}
	if allowed, _ := limiter.allow(newIP, started.Add(time.Minute)); !allowed {
		t.Fatal("expired entries did not free capacity")
	}
	if len(limiter.clients) != 2 {
		t.Fatalf("retained %d clients after expiry, want 2", len(limiter.clients))
	}
	if allowed, _ := limiter.allow(first, started.Add(time.Minute)); !allowed {
		t.Fatal("cleanup removed an active renewed window")
	}
}

func TestAuthClientIP(t *testing.T) {
	for _, tt := range []struct {
		remoteAddr string
		want       string
	}{
		{"192.0.2.1:1234", "192.0.2.1"},
		{"192.0.2.1:5678", "192.0.2.1"},
		{"[::ffff:192.0.2.1]:1234", "192.0.2.1"},
		{"[2001:db8::1]:1234", "2001:db8::1"},
		{"[fe80::1%eth0]:1234", "fe80::1"},
		{"2001:db8::1", "2001:db8::1"},
		{"192.0.2.1", "192.0.2.1"},
		{"", "invalid IP"},
		{"not-an-address", "invalid IP"},
	} {
		t.Run(tt.remoteAddr, func(t *testing.T) {
			if got := authClientIP(tt.remoteAddr).String(); got != tt.want {
				t.Fatalf("IP=%q want=%q", got, tt.want)
			}
		})
	}
}

type rateLimitAuthService struct{ calls atomic.Int64 }

func (s *rateLimitAuthService) Register(context.Context, service.RegisterInput) (domain.User, error) {
	s.calls.Add(1)
	return domain.User{ID: 1}, nil
}

func (s *rateLimitAuthService) Login(context.Context, service.LoginInput) (domain.AccessToken, error) {
	s.calls.Add(1)
	return domain.AccessToken{}, domain.ErrInvalidCredentials
}

func (*rateLimitAuthService) Me(context.Context, int64) (domain.User, error) {
	return domain.User{ID: 1}, nil
}

func TestAuthRateLimitRoutes(t *testing.T) {
	svc := &rateLimitAuthService{}
	router := NewRouter(5*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, NewAuthHandler(svc, nil), nil, nil)
	for i := range authAttemptsPerWindow + 2 {
		path, wantStatus := "/api/v1/auth/login", http.StatusUnauthorized
		if i%2 == 0 {
			path, wantStatus = "/api/v1/auth/register", http.StatusCreated
		}
		if i >= authAttemptsPerWindow {
			wantStatus = http.StatusTooManyRequests
		}
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		r.RemoteAddr = "192.0.2.1:" + strconv.Itoa(1000+i)
		// Different source ports and untrusted proxy headers cannot reset the budget.
		if i >= authAttemptsPerWindow {
			r.RemoteAddr = "[::ffff:192.0.2.1]:9999"
		}
		r.Header.Set("X-Forwarded-For", "203.0.113."+strconv.Itoa(i+1))
		r.Header.Set("X-Real-IP", "203.0.113."+strconv.Itoa(i+1))
		r.Header.Set("Forwarded", "for=203.0.113."+strconv.Itoa(i+1))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != wantStatus {
			t.Fatalf("attempt %d: status=%d want=%d", i+1, w.Code, wantStatus)
		}
		if wantStatus == http.StatusTooManyRequests {
			assertAuthRateLimited(t, w)
		}
	}
	if got := svc.calls.Load(); got != authAttemptsPerWindow {
		t.Fatalf("service called %d times, want %d", got, authAttemptsPerWindow)
	}
	for _, tt := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/health", http.StatusOK},
		{http.MethodGet, "/ready", http.StatusServiceUnavailable},
		{http.MethodGet, "/api/v1/users/me", http.StatusUnauthorized},
		{http.MethodGet, "/api/v1/auth/login", http.StatusMethodNotAllowed},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(tt.method, tt.path, nil)
		r.RemoteAddr = "192.0.2.1:9999"
		router.ServeHTTP(w, r)
		if w.Code != tt.status || w.Header().Get("Retry-After") != "" {
			t.Fatalf("%s %s: status=%d", tt.method, tt.path, w.Code)
		}
	}
}

func TestAuthRateLimitOverHTTP(t *testing.T) {
	svc := &rateLimitAuthService{}
	router := NewRouter(5*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, NewAuthHandler(svc, nil), nil, nil)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 5 * time.Second
	for i := range authAttemptsPerWindow + 1 {
		// Malformed bodies consume the budget before JSON decoding or service work.
		body, want := "{", http.StatusBadRequest
		if i == authAttemptsPerWindow {
			body, want = `{}`, http.StatusTooManyRequests
		}
		response, err := client.Post(server.URL+"/api/v1/auth/login", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		_, readErr := io.Copy(io.Discard, response.Body)
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("read=%v close=%v", readErr, closeErr)
		}
		if response.StatusCode != want {
			t.Fatalf("attempt %d status=%d want=%d", i+1, response.StatusCode, want)
		}
	}
	if svc.calls.Load() != 0 {
		t.Fatal("malformed or rate-limited request reached the service")
	}
}

func TestAuthRateLimitRetryAfterRounding(t *testing.T) {
	for _, tt := range []struct {
		remaining time.Duration
		want      string
	}{
		{time.Nanosecond, "1"},
		{500 * time.Millisecond, "1"},
		{time.Second, "1"},
		{1500 * time.Millisecond, "2"},
	} {
		t.Run(tt.remaining.String(), func(t *testing.T) {
			limiter := newAuthRateLimiter()
			ip := netip.MustParseAddr("192.0.2.1")
			now := time.Now()
			limiter.now = func() time.Time { return now }
			limiter.clients[ip] = authWindow{attempts: authAttemptsPerWindow, resetAt: now.Add(tt.remaining)}
			next := limiter.middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Error("blocked request reached handler")
			}))
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
			r.RemoteAddr = "192.0.2.1:1234"
			next.ServeHTTP(w, r)
			assertAuthRateLimited(t, w)
			if got := w.Header().Get("Retry-After"); got != tt.want {
				t.Fatalf("Retry-After=%q want %s", got, tt.want)
			}
		})
	}
}

func TestAuthRateLimitMalformedPeers(t *testing.T) {
	limiter := newAuthRateLimiter()
	var calls int
	next := limiter.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	}))
	for i := range authAttemptsPerWindow + 1 {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		r.RemoteAddr = "invalid-peer-" + strconv.Itoa(i)
		next.ServeHTTP(w, r)
		if i == authAttemptsPerWindow {
			assertAuthRateLimited(t, w)
		}
	}
	if calls != authAttemptsPerWindow {
		t.Fatalf("malformed peers bypassed the shared budget: calls=%d", calls)
	}
}

func assertAuthRateLimited(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Content-Type") != "application/json" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unsafe rate-limit response: status=%d headers=%v", w.Code, w.Header())
	}
	if seconds, err := strconv.Atoi(w.Header().Get("Retry-After")); err != nil || seconds < 1 {
		t.Fatal("missing positive Retry-After")
	}
	var response struct {
		Error struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Error.Code != "rate_limited" || response.Error.Message != "Too many authentication attempts" || response.Error.Details == nil || len(response.Error.Details) != 0 {
		t.Fatalf("unexpected error envelope: %s", w.Body.String())
	}
}
