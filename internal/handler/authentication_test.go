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

	"github.com/golang-jwt/jwt/v5"
	"github.com/kanatovs/taskflow-api/internal/domain"
	"github.com/kanatovs/taskflow-api/internal/platform/token"
	"github.com/kanatovs/taskflow-api/internal/service"
)

type currentUserRepository struct {
	registerRepository
	lookupErr error
	lookups   int
}

func (r *currentUserRepository) ByID(ctx context.Context, id int64) (domain.User, error) {
	r.lookups++
	return domain.User{ID: id, Email: "user@example.com", DisplayName: "User", PasswordHash: "private-hash"}, r.lookupErr
}

func TestAuthenticatedMeHTTP(t *testing.T) {
	const secret = "unit-test-secret-at-least-32-bytes"
	verifier, err := token.NewJWT(secret, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	access, err := verifier.Issue(42)
	if err != nil {
		t.Fatal(err)
	}
	expired, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{Subject: "42", IssuedAt: jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)), ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour))}).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	wrongIssuer, err := token.NewJWT("another-secret-at-least-32-bytes-long", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := wrongIssuer.Issue(42)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, authorization string
		repoErr             error
		status, lookups     int
	}{
		{"valid", "Bearer " + access.AccessToken, nil, 200, 1},
		{"case insensitive scheme", "bearer " + access.AccessToken, nil, 200, 1},
		{"missing header", "", nil, 401, 0},
		{"missing token", "Bearer", nil, 401, 0},
		{"wrong scheme", "Basic " + access.AccessToken, nil, 401, 0},
		{"extra parts", "Bearer " + access.AccessToken + " extra", nil, 401, 0},
		{"tab separator", "Bearer\t" + access.AccessToken, nil, 401, 0},
		{"malformed token", "Bearer invalid", nil, 401, 0},
		{"wrong signature", "Bearer " + wrong.AccessToken, nil, 401, 0},
		{"expired token", "Bearer " + expired, nil, 401, 0},
		{"deleted user", "Bearer " + access.AccessToken, domain.ErrNotFound, 401, 1},
		{"repository failure", "Bearer " + access.AccessToken, errors.New("private database failure"), 500, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			repo := &currentUserRepository{lookupErr: tt.repoErr}
			router := NewRouter(slog.New(slog.NewJSONHandler(&logs, nil)), nil, NewAuthHandler(service.NewAuthService(repo, verifier), verifier), nil, nil)
			request := httptest.NewRequest("GET", "/api/v1/users/me", nil)
			if tt.authorization != "" {
				request.Header.Set("Authorization", tt.authorization)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, request)
			if w.Code != tt.status || repo.lookups != tt.lookups {
				t.Fatalf("status=%d lookups=%d body=%s", w.Code, repo.lookups, w.Body.String())
			}
			if tt.status == 401 && w.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Fatal("missing Bearer challenge")
			}
			for _, sensitive := range []string{access.AccessToken, "private-hash", "password_hash", "private database failure"} {
				if strings.Contains(w.Body.String(), sensitive) || strings.Contains(logs.String(), sensitive) {
					t.Fatal("sensitive data exposed")
				}
			}
			if tt.status == 200 && !strings.Contains(w.Body.String(), `"id":42`) {
				t.Fatal("wrong authenticated user")
			}
		})
	}
}
