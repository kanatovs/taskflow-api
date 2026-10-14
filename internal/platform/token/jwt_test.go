package token

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "unit-test-secret-at-least-32-bytes"

func TestIssue(t *testing.T) {
	issuer, err := NewJWT(testSecret, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	access, err := issuer.Issue(42)
	if err != nil {
		t.Fatal(err)
	}
	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(access.AccessToken, claims, func(*jwt.Token) (any, error) { return []byte(testSecret), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil || !parsed.Valid {
		t.Fatalf("issued token invalid: %v", err)
	}
	if len(claims) != 3 || claims["sub"] != "42" || claims["iat"] == nil || claims["exp"] == nil {
		t.Fatal("unexpected claim set")
	}
	if access.TokenType != "Bearer" || access.ExpiresIn != 900 {
		t.Fatal("unexpected token metadata")
	}
	if _, err := issuer.Issue(0); err == nil {
		t.Fatal("invalid user ID accepted")
	}
}

func TestNewJWTValidation(t *testing.T) {
	if _, err := NewJWT("short", time.Minute); err == nil {
		t.Fatal("short secret accepted")
	}
	if _, err := NewJWT(testSecret, 0); err == nil {
		t.Fatal("zero lifetime accepted")
	}
}
