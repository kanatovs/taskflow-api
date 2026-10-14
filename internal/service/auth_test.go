package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kanatovs/taskflow-api/internal/domain"
	"golang.org/x/crypto/bcrypt"
)

type userRepositoryStub struct {
	create  func(context.Context, domain.User) (domain.User, error)
	byEmail func(context.Context, string) (domain.User, error)
	byID    func(context.Context, int64) (domain.User, error)
}

func (r userRepositoryStub) ByID(ctx context.Context, id int64) (domain.User, error) {
	return r.byID(ctx, id)
}

func (r userRepositoryStub) ByEmail(ctx context.Context, email string) (domain.User, error) {
	return r.byEmail(ctx, email)
}

func (r userRepositoryStub) Create(ctx context.Context, user domain.User) (domain.User, error) {
	return r.create(ctx, user)
}

func validRegistration() RegisterInput {
	return RegisterInput{Email: " User@Example.COM ", Password: "strong-password-123", DisplayName: " Test User "}
}

func TestRegister(t *testing.T) {
	ctx := context.WithValue(context.Background(), struct{}{}, "request")
	repo := userRepositoryStub{create: func(gotCtx context.Context, user domain.User) (domain.User, error) {
		if gotCtx != ctx {
			t.Error("context was not propagated")
		}
		if user.Email != "user@example.com" || user.DisplayName != "Test User" {
			t.Errorf("input not normalized: %+v", user)
		}
		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(validRegistration().Password)); err != nil {
			t.Errorf("password was not hashed: %v", err)
		}
		user.ID = 1
		return user, nil
	}}
	user, err := NewAuthService(repo, nil).Register(ctx, validRegistration())
	if err != nil || user.ID != 1 {
		t.Fatalf("Register: %v", err)
	}
}

func TestRegisterValidation(t *testing.T) {
	cases := []struct {
		name   string
		change func(*RegisterInput)
		field  string
	}{
		{"empty email", func(i *RegisterInput) { i.Email = "" }, "email"},
		{"invalid email", func(i *RegisterInput) { i.Email = "not-an-email" }, "email"},
		{"display address", func(i *RegisterInput) { i.Email = "Name <user@example.com>" }, "email"},
		{"invalid domain", func(i *RegisterInput) { i.Email = "user@-example.com" }, "email"},
		{"empty name", func(i *RegisterInput) { i.DisplayName = "  " }, "display_name"},
		{"long name", func(i *RegisterInput) { i.DisplayName = strings.Repeat("a", 101) }, "display_name"},
		{"control in name", func(i *RegisterInput) { i.DisplayName = "User\nName" }, "display_name"},
		{"short password", func(i *RegisterInput) { i.Password = "short" }, "password"},
		{"long password", func(i *RegisterInput) { i.Password = strings.Repeat("a", 73) }, "password"},
		{"blank password", func(i *RegisterInput) { i.Password = strings.Repeat(" ", 12) }, "password"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			input := validRegistration()
			tt.change(&input)
			repo := userRepositoryStub{create: func(context.Context, domain.User) (domain.User, error) {
				t.Fatal("repository called for invalid input")
				return domain.User{}, nil
			}}
			_, err := NewAuthService(repo, nil).Register(context.Background(), input)
			var validation *domain.ValidationError
			if !errors.As(err, &validation) || validation.Fields[tt.field] == "" {
				t.Fatalf("validation: %v", err)
			}
		})
	}
}

func TestRegisterRepositoryErrors(t *testing.T) {
	for _, failure := range []error{domain.ErrEmailExists, errors.New("repository failure")} {
		repo := userRepositoryStub{create: func(context.Context, domain.User) (domain.User, error) { return domain.User{}, failure }}
		if _, err := NewAuthService(repo, nil).Register(context.Background(), validRegistration()); !errors.Is(err, failure) {
			t.Fatalf("error lost: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewAuthService(userRepositoryStub{}, nil).Register(ctx, validRegistration()); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled request processed")
	}
}
