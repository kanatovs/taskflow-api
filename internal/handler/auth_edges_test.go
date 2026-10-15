package handler

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kanatovs/taskflow-api/internal/platform/token"
	"github.com/kanatovs/taskflow-api/internal/service"
)

func TestAuthenticationHeaderEdges(t *testing.T) {
	issuer, err := token.NewJWT("unit-test-secret-at-least-32-bytes", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	access, err := issuer.Issue(42)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name    string
		headers []string
		status  int
	}{
		{"duplicate headers", []string{"Bearer " + access.AccessToken, "Bearer " + access.AccessToken}, 401},
		{"leading whitespace", []string{" Bearer " + access.AccessToken}, 401},
		{"trailing whitespace", []string{"Bearer " + access.AccessToken + " "}, 401},
		{"empty token", []string{"Bearer "}, 401},
		{"oversized token", []string{"Bearer " + strings.Repeat("a", 4097)}, 401},
		{"multiple spaces", []string{"Bearer   " + access.AccessToken}, 200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &currentUserRepository{}
			router := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), nil, NewAuthHandler(service.NewAuthService(repo, issuer), issuer), nil, nil)
			request := httptest.NewRequest("GET", "/api/v1/users/me", nil)
			for _, header := range tt.headers {
				request.Header.Add("Authorization", header)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, request)
			if w.Code != tt.status {
				t.Fatalf("status=%d", w.Code)
			}
			if tt.status == 401 && repo.lookups != 0 {
				t.Fatal("invalid authentication reached repository")
			}
		})
	}
}

func TestLoginJSONEdges(t *testing.T) {
	for _, body := range []string{"", `[]`, `null`, `{}{"email":"user@example.com"}`, `{"password":false}`, `{"email":"` + strings.Repeat("a", 5000) + `"}`} {
		repo := &registerRepository{}
		router := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), nil, NewAuthHandler(service.NewAuthService(repo, nil), nil), nil, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("status=%d for malformed login", w.Code)
		}
	}
}

func TestReadinessTimeout(t *testing.T) {
	pinger := pingFunc(func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() })
	w := httptest.NewRecorder()
	NewReadiness(pinger, 10*time.Millisecond).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if w.Code != 503 {
		t.Fatalf("status=%d", w.Code)
	}
}
