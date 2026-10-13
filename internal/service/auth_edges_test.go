package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kanatovs/taskflow-api/internal/domain"
	"golang.org/x/crypto/bcrypt"
)

func TestRegistrationBoundaries(t *testing.T) {
	for _, length := range []int{12, 72} {
		input := RegisterInput{Email: "edge@example.com", Password: strings.Repeat("a", length), DisplayName: strings.Repeat("Ж", 100)}
		repo := userRepositoryStub{create: func(_ context.Context, user domain.User) (domain.User, error) {
			if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(input.Password)); err != nil {
				t.Fatal("boundary password altered")
			}
			return user, nil
		}}
		if _, err := NewAuthService(repo, nil).Register(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	}
	for _, email := range []string{"user@host", "user@bad_domain.com", "user@example..com", "user@例子.com", strings.Repeat("a", 65) + "@example.com", strings.Repeat("a", 255) + "@example.com"} {
		if validEmail(email) {
			t.Fatalf("invalid email accepted: %q", email)
		}
	}
}

func TestAuthenticationCancellationAndCorruptHash(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	auth := NewAuthService(userRepositoryStub{}, nil)
	if _, err := auth.Login(ctx, LoginInput{Email: "user@example.com", Password: "strong-password-123"}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled login processed")
	}
	if _, err := auth.Me(ctx, 42); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled current user lookup processed")
	}
	repo := userRepositoryStub{byEmail: func(context.Context, string) (domain.User, error) {
		return domain.User{ID: 42, PasswordHash: "corrupt-hash"}, nil
	}}
	if _, err := NewAuthService(repo, nil).Login(context.Background(), LoginInput{Email: "user@example.com", Password: "strong-password-123"}); err == nil || errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatal("corrupt stored hash must be an internal failure")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(dummyPasswordHash), []byte("definitely-not-dummy-password")); !errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		t.Fatal("dummy hash must support real bcrypt comparison")
	}
}
