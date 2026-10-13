package service

import (
	"context"
	"fmt"
	"net/mail"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kanatovs/taskflow-api/internal/domain"
	"golang.org/x/crypto/bcrypt"
)

type UserRepository interface {
	Create(context.Context, domain.User) (domain.User, error)
}

type AuthService struct{ users UserRepository }

func NewAuthService(users UserRepository) *AuthService { return &AuthService{users: users} }

type RegisterInput struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

func (s *AuthService) Register(ctx context.Context, input RegisterInput) (domain.User, error) {
	if err := ctx.Err(); err != nil {
		return domain.User{}, err
	}
	input.Email = normalizeEmail(input.Email)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	fields := make(map[string]string)
	if !validEmail(input.Email) {
		fields["email"] = "must be a valid ASCII email address of at most 254 bytes"
	}
	if !utf8.ValidString(input.DisplayName) || utf8.RuneCountInString(input.DisplayName) < 1 || utf8.RuneCountInString(input.DisplayName) > 100 || strings.IndexFunc(input.DisplayName, unicode.IsControl) >= 0 {
		fields["display_name"] = "must contain 1 to 100 characters without control characters"
	}
	if len(input.Password) < 12 || len(input.Password) > 72 || !utf8.ValidString(input.Password) || strings.TrimSpace(input.Password) == "" {
		fields["password"] = "must contain 12 to 72 bytes"
	}
	if len(fields) != 0 {
		return domain.User{}, &domain.ValidationError{Fields: fields}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return domain.User{}, fmt.Errorf("hash registration password: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return domain.User{}, err
	}
	user, err := s.users.Create(ctx, domain.User{Email: input.Email, PasswordHash: string(hash), DisplayName: input.DisplayName})
	if err != nil {
		return domain.User{}, fmt.Errorf("register user: %w", err)
	}
	return user, nil
}

func normalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

func validEmail(email string) bool {
	if len(email) > 254 || strings.IndexFunc(email, func(r rune) bool { return r > 127 || unicode.IsSpace(r) }) >= 0 {
		return false
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return false
	}
	separator := strings.LastIndexByte(email, '@')
	if separator < 1 || separator > 64 {
		return false
	}
	domainName := email[separator+1:]
	if !strings.Contains(domainName, ".") {
		return false
	}
	for _, label := range strings.Split(domainName, ".") {
		if len(label) < 1 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}
