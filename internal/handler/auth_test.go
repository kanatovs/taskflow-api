package handler

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kanatovs/taskflow-api/internal/domain"
	"github.com/kanatovs/taskflow-api/internal/service"
)

type registerRepository struct {
	err   error
	calls int
}

func (r *registerRepository) ByEmail(context.Context, string) (domain.User, error) {
	return domain.User{}, domain.ErrNotFound
}

func (r *registerRepository) ByID(context.Context, int64) (domain.User, error) {
	return domain.User{}, domain.ErrNotFound
}

func (r *registerRepository) Create(_ context.Context, user domain.User) (domain.User, error) {
	r.calls++
	user.ID = 1
	return user, r.err
}

func TestRegistrationHTTP(t *testing.T) {
	valid := `{"email":" User@Example.com ","password":"strong-password-123","display_name":"User"}`
	for _, tt := range []struct {
		name, body string
		err        error
		status     int
		calls      int
	}{
		{"success", valid, nil, 201, 1},
		{"malformed", `{"email":`, nil, 400, 0},
		{"unknown field", `{"admin":true}`, nil, 400, 0},
		{"trailing object", valid + `{}`, nil, 400, 0},
		{"wrong type", `{"email":123}`, nil, 400, 0},
		{"null", `null`, nil, 400, 0},
		{"empty object", `{}`, nil, 400, 0},
		{"oversized", `{"email":"` + strings.Repeat("a", 5000) + `"}`, nil, 400, 0},
		{"duplicate", valid, domain.ErrEmailExists, 409, 1},
		{"repository failure", valid, errors.New("private-password-hash"), 500, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			repo := &registerRepository{err: tt.err}
			router := NewRouter(slog.New(slog.NewJSONHandler(&logs, nil)), nil, NewAuthHandler(service.NewAuthService(repo, nil), nil), nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/auth/register", strings.NewReader(tt.body)))
			if w.Code != tt.status || repo.calls != tt.calls {
				t.Fatalf("status=%d calls=%d body=%s", w.Code, repo.calls, w.Body.String())
			}
			if w.Header().Get("Content-Type") != "application/json" {
				t.Fatal("missing JSON content type")
			}
			for _, secret := range []string{"strong-password-123", "password_hash", "private-password-hash", "$2a$"} {
				if strings.Contains(w.Body.String(), secret) || strings.Contains(logs.String(), secret) {
					t.Fatalf("sensitive value exposed: %s", secret)
				}
			}
		})
	}
}
