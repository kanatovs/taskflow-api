package handler_test

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kanatovs/taskflow-api/internal/handler"
	"github.com/kanatovs/taskflow-api/internal/repository/postgres"
	"github.com/kanatovs/taskflow-api/internal/service"
	"github.com/kanatovs/taskflow-api/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

func TestRegistrationPostgresIntegration(t *testing.T) {
	pool := testutil.Postgres(t)
	repo := postgres.NewUserRepository(pool, time.Second)
	router := handler.NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), handler.NewReadiness(pool, time.Second), handler.NewAuthHandler(service.NewAuthService(repo)))
	body := `{"email":" User@Example.com ","password":"strong-password-123","display_name":"Test User"}`
	for _, want := range []int{201, 409} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/auth/register", strings.NewReader(body)))
		if w.Code != want {
			t.Fatalf("register status=%d body=%s", w.Code, w.Body.String())
		}
	}
	user, err := repo.ByEmail(context.Background(), "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("strong-password-123")); err != nil {
		t.Fatal("stored password is not bcrypt")
	}
}
