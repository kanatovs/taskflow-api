package database

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/kanatovs/taskflow-api/internal/handler"
)

func TestPostgresReadinessIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("set TEST_POSTGRES_URL to the disposable Compose PostgreSQL")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
		t.Fatal("integration database must be local")
	}
	pool, err := New(context.Background(), dsn, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	router := handler.NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), handler.NewReadiness(pool, time.Second), nil)
	check := func(path string, want int) {
		t.Helper()
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != want {
			t.Fatalf("%s status=%d want=%d", path, w.Code, want)
		}
	}
	check("/ready", 200)
	pool.Close()
	check("/ready", 503)
	check("/health", 200)
}
