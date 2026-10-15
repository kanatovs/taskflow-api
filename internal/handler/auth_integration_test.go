package handler_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/kanatovs/taskflow-api/internal/domain"
	"github.com/kanatovs/taskflow-api/internal/handler"
	"github.com/kanatovs/taskflow-api/internal/platform/token"
	"github.com/kanatovs/taskflow-api/internal/repository/postgres"
	"github.com/kanatovs/taskflow-api/internal/service"
	"github.com/kanatovs/taskflow-api/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

func TestRegistrationPostgresIntegration(t *testing.T) {
	pool := testutil.Postgres(t)
	repo := postgres.NewUserRepository(pool, time.Second)
	issuer, err := token.NewJWT("unit-test-secret-at-least-32-bytes", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	router := handler.NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), handler.NewReadiness(pool, time.Second), handler.NewAuthHandler(service.NewAuthService(repo, issuer), issuer), nil, nil)
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
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"email":"USER@example.com","password":"strong-password-123"}`)))
	if w.Code != 200 {
		t.Fatalf("login status=%d", w.Code)
	}
	var response struct {
		Data domain.AccessToken `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	claims := &jwt.RegisteredClaims{}
	parsed, err := jwt.ParseWithClaims(response.Data.AccessToken, claims, func(*jwt.Token) (any, error) { return []byte("unit-test-secret-at-least-32-bytes"), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
	if err != nil || !parsed.Valid || claims.Subject == "" {
		t.Fatalf("invalid login JWT: %v", err)
	}
	me := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/api/v1/users/me", nil)
	request.Header.Set("Authorization", "Bearer "+response.Data.AccessToken)
	router.ServeHTTP(me, request)
	if me.Code != 200 || strings.Contains(me.Body.String(), "password") {
		t.Fatalf("me status=%d body=%s", me.Code, me.Body.String())
	}
}
