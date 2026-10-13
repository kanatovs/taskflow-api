package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kanatovs/taskflow-api/internal/domain"
	"github.com/kanatovs/taskflow-api/internal/platform/token"
	"golang.org/x/crypto/bcrypt"
)

type failingIssuer struct{ err error }

func (i failingIssuer) Issue(int64) (domain.AccessToken, error) { return domain.AccessToken{}, i.err }

func TestLogin(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("strong-password-123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := token.NewJWT("unit-test-secret-at-least-32-bytes", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("repository failure")
	for _, tt := range []struct {
		name, email, password, hash string
		repoErr, wantErr            error
	}{
		{"success", " User@Example.com ", "strong-password-123", string(hash), nil, nil},
		{"wrong password", "user@example.com", "wrong-password", string(hash), nil, domain.ErrInvalidCredentials},
		{"unknown email", "user@example.com", "strong-password-123", "", domain.ErrNotFound, domain.ErrInvalidCredentials},
		{"repository failure", "user@example.com", "strong-password-123", "", failure, failure},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), struct{}{}, "request")
			repo := userRepositoryStub{byEmail: func(gotCtx context.Context, email string) (domain.User, error) {
				if email != "user@example.com" || gotCtx != ctx {
					t.Fatal("email normalization or context propagation failed")
				}
				return domain.User{ID: 1, PasswordHash: tt.hash}, tt.repoErr
			}}
			access, err := NewAuthService(repo, issuer).Login(ctx, LoginInput{Email: tt.email, Password: tt.password})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Login error=%v", err)
			}
			if tt.wantErr == nil && access.AccessToken == "" {
				t.Fatal("missing token")
			}
		})
	}
	if cost, err := bcrypt.Cost([]byte(dummyPasswordHash)); err != nil || cost != bcrypt.DefaultCost {
		t.Fatal("dummy hash must match registration cost")
	}
}

func TestLoginValidationAndFailures(t *testing.T) {
	for _, input := range []LoginInput{{Email: "invalid", Password: "password"}, {Email: "user@example.com"}} {
		_, err := NewAuthService(userRepositoryStub{}, nil).Login(context.Background(), input)
		var validation *domain.ValidationError
		if !errors.As(err, &validation) {
			t.Fatalf("validation: %v", err)
		}
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("strong-password-123"), bcrypt.DefaultCost)
	failure := errors.New("token issuance failure")
	repo := userRepositoryStub{byEmail: func(context.Context, string) (domain.User, error) {
		return domain.User{ID: 1, PasswordHash: string(hash)}, nil
	}}
	if _, err := NewAuthService(repo, failingIssuer{failure}).Login(context.Background(), LoginInput{Email: "user@example.com", Password: "strong-password-123"}); !errors.Is(err, failure) {
		t.Fatalf("token failure lost: %v", err)
	}
}
