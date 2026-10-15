package handler

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kanatovs/taskflow-api/internal/domain"
	"github.com/kanatovs/taskflow-api/internal/platform/token"
	"github.com/kanatovs/taskflow-api/internal/service"
	"golang.org/x/crypto/bcrypt"
)

type loginRepository struct {
	registerRepository
	user      domain.User
	lookupErr error
}

func (r *loginRepository) ByEmail(context.Context, string) (domain.User, error) {
	return r.user, r.lookupErr
}

func TestLoginHTTP(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("strong-password-123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := token.NewJWT("unit-test-secret-at-least-32-bytes", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var invalidResponse string
	for _, tt := range []struct {
		name, body string
		repoErr    error
		status     int
	}{
		{"success", `{"email":"USER@example.com","password":"strong-password-123"}`, nil, 200},
		{"wrong password", `{"email":"user@example.com","password":"wrong-password"}`, nil, 401},
		{"unknown email", `{"email":"user@example.com","password":"strong-password-123"}`, domain.ErrNotFound, 401},
		{"malformed", `{`, nil, 400},
		{"unknown field", `{"extra":true}`, nil, 400},
		{"invalid fields", `{"email":"invalid","password":""}`, nil, 400},
		{"repository failure", `{"email":"user@example.com","password":"strong-password-123"}`, errors.New("private detail"), 500},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			repo := &loginRepository{user: domain.User{ID: 1, PasswordHash: string(hash)}, lookupErr: tt.repoErr}
			router := NewRouter(slog.New(slog.NewJSONHandler(&logs, nil)), nil, NewAuthHandler(service.NewAuthService(repo, issuer), issuer), nil, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(tt.body)))
			if w.Code != tt.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if tt.status == 401 {
				if invalidResponse == "" {
					invalidResponse = w.Body.String()
				} else if invalidResponse != w.Body.String() {
					t.Fatal("credential failure disclosed account existence")
				}
			}
			if tt.status == 200 && w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("token response cacheable")
			}
			for _, secret := range []string{"strong-password-123", string(hash), "private detail"} {
				if strings.Contains(w.Body.String(), secret) || strings.Contains(logs.String(), secret) {
					t.Fatal("secret exposed")
				}
			}
		})
	}
}
