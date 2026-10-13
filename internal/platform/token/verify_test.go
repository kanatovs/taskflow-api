package token

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/kanatovs/taskflow-api/internal/domain"
)

func TestVerify(t *testing.T) {
	verifier, err := NewJWT(testSecret, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := verifier.Issue(42)
	if err != nil {
		t.Fatal(err)
	}
	if id, err := verifier.Verify(issued.AccessToken); err != nil || id != 42 {
		t.Fatalf("valid JWT: %v", err)
	}
	now := time.Now()
	validClaims := func() jwt.RegisteredClaims {
		return jwt.RegisteredClaims{Subject: "42", IssuedAt: jwt.NewNumericDate(now.Add(-time.Minute)), ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute))}
	}
	for _, tt := range []struct {
		name   string
		method jwt.SigningMethod
		secret any
		change func(*jwt.RegisteredClaims)
	}{
		{"invalid signature", jwt.SigningMethodHS256, []byte("another-secret-at-least-32-bytes-long"), nil},
		{"wrong HMAC algorithm", jwt.SigningMethodHS384, []byte(testSecret), nil},
		{"unsigned", jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, nil},
		{"expired", jwt.SigningMethodHS256, []byte(testSecret), func(c *jwt.RegisteredClaims) { c.ExpiresAt = jwt.NewNumericDate(now.Add(-time.Second)) }},
		{"missing expiration", jwt.SigningMethodHS256, []byte(testSecret), func(c *jwt.RegisteredClaims) { c.ExpiresAt = nil }},
		{"missing issued at", jwt.SigningMethodHS256, []byte(testSecret), func(c *jwt.RegisteredClaims) { c.IssuedAt = nil }},
		{"future issued at", jwt.SigningMethodHS256, []byte(testSecret), func(c *jwt.RegisteredClaims) { c.IssuedAt = jwt.NewNumericDate(now.Add(30 * time.Second)) }},
		{"invalid subject", jwt.SigningMethodHS256, []byte(testSecret), func(c *jwt.RegisteredClaims) { c.Subject = "email@example.com" }},
		{"zero subject", jwt.SigningMethodHS256, []byte(testSecret), func(c *jwt.RegisteredClaims) { c.Subject = "0" }},
		{"noncanonical subject", jwt.SigningMethodHS256, []byte(testSecret), func(c *jwt.RegisteredClaims) { c.Subject = "042" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			claims := validClaims()
			if tt.change != nil {
				tt.change(&claims)
			}
			encoded, err := jwt.NewWithClaims(tt.method, claims).SignedString(tt.secret)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := verifier.Verify(encoded); !errors.Is(err, domain.ErrUnauthenticated) {
				t.Fatalf("invalid JWT accepted: %v", err)
			}
		})
	}
	if _, err := verifier.Verify("malformed"); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("malformed token accepted")
	}
}
