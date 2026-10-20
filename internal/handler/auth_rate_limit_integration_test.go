package handler_test

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

	"github.com/kanatovs/taskflow-api/internal/domain"
	"github.com/kanatovs/taskflow-api/internal/handler"
	"github.com/kanatovs/taskflow-api/internal/repository/postgres"
	"github.com/kanatovs/taskflow-api/internal/service"
	"github.com/kanatovs/taskflow-api/internal/testutil"
)

func TestAuthRateLimitPostgresIntegration(t *testing.T) {
	pool := testutil.Postgres(t)
	users := postgres.NewUserRepository(pool, time.Second)
	router := handler.NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), nil,
		handler.NewAuthHandler(service.NewAuthService(users, nil), nil), nil, nil)
	post := func(path, body, peer string, want int) {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.RemoteAddr = peer
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("POST %s status=%d want=%d", path, w.Code, want)
		}
	}
	for range 10 {
		post("/api/v1/auth/login", "{", "192.0.2.1:1234", http.StatusBadRequest)
	}
	registration := `{"email":"limited@example.com","password":"strong-password-123","display_name":"Limited"}`
	post("/api/v1/auth/register", registration, "192.0.2.1:5678", http.StatusTooManyRequests)
	if _, err := users.ByEmail(context.Background(), "limited@example.com"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("rate-limited registration changed persistence: %v", err)
	}
	post("/api/v1/auth/register", registration, "192.0.2.2:1234", http.StatusCreated)
	if _, err := users.ByEmail(context.Background(), "limited@example.com"); err != nil {
		t.Fatalf("independent IP could not register: %v", err)
	}
}
