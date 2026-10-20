package handler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type pingFunc func(context.Context) error

func (f pingFunc) Ping(ctx context.Context) error { return f(ctx) }

func TestReadinessAndLiveness(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		calls := 0
		pinger := pingFunc(func(ctx context.Context) error {
			calls++
			if _, ok := ctx.Deadline(); !ok {
				t.Error("missing database deadline")
			}
			if unavailable {
				return errors.New("private connection detail")
			}
			return nil
		})
		router := NewRouter(5*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)), NewReadiness(pinger, time.Second), nil, nil, nil)
		ready := httptest.NewRecorder()
		router.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/ready", nil))
		want := 200
		if unavailable {
			want = 503
		}
		if ready.Code != want || calls != 1 {
			t.Fatalf("status=%d calls=%d", ready.Code, calls)
		}
		if strings.Contains(ready.Body.String(), "private") {
			t.Fatal("internal detail exposed")
		}
		health := httptest.NewRecorder()
		router.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health", nil))
		if health.Code != 200 || calls != 1 {
			t.Fatal("liveness depends on database")
		}
	}
}

func TestReadinessCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pinger := pingFunc(func(ctx context.Context) error { return ctx.Err() })
	w := httptest.NewRecorder()
	NewReadiness(pinger, time.Second).ServeHTTP(w, httptest.NewRequest("GET", "/ready", nil).WithContext(ctx))
	if w.Code != 503 {
		t.Fatalf("status=%d", w.Code)
	}
}
