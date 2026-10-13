package token

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/kanatovs/taskflow-api/internal/domain"
)

type JWT struct {
	secret []byte
	ttl    time.Duration
}

func NewJWT(secret string, ttl time.Duration) (*JWT, error) {
	if len(secret) < 32 || strings.TrimSpace(secret) == "" {
		return nil, errors.New("JWT secret must contain at least 32 bytes")
	}
	if ttl < time.Second {
		return nil, errors.New("JWT lifetime must be at least one second")
	}
	return &JWT{secret: []byte(secret), ttl: ttl}, nil
}

func (j *JWT) Issue(userID int64) (domain.AccessToken, error) {
	if userID <= 0 {
		return domain.AccessToken{}, errors.New("invalid token subject")
	}
	now := time.Now().UTC()
	claims := jwt.RegisteredClaims{
		Subject:   strconv.FormatInt(userID, 10),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(j.ttl)),
	}
	encoded, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(j.secret)
	if err != nil {
		return domain.AccessToken{}, fmt.Errorf("sign access token: %w", err)
	}
	return domain.AccessToken{AccessToken: encoded, TokenType: "Bearer", ExpiresIn: int64(j.ttl / time.Second)}, nil
}

func (j *JWT) Verify(encoded string) (int64, error) {
	claims := &jwt.RegisteredClaims{}
	parsed, err := jwt.ParseWithClaims(encoded, claims, func(parsed *jwt.Token) (any, error) {
		if parsed.Method != jwt.SigningMethodHS256 {
			return nil, domain.ErrUnauthenticated
		}
		return j.secret, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil || !parsed.Valid || claims.IssuedAt == nil || claims.ExpiresAt == nil || !claims.ExpiresAt.After(claims.IssuedAt.Time) {
		return 0, domain.ErrUnauthenticated
	}
	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || userID <= 0 || strconv.FormatInt(userID, 10) != claims.Subject {
		return 0, domain.ErrUnauthenticated
	}
	return userID, nil
}
