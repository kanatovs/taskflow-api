package handler

import (
	"context"
	"net/http"
	"strings"
	"unicode"

	"github.com/kanatovs/taskflow-api/internal/domain"
)

type userIDContextKey struct{}

func (h *AuthHandler) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		unauthorized := func() { writeServiceError(w, r, "authenticate request", domain.ErrUnauthenticated) }
		headers := r.Header.Values("Authorization")
		if len(headers) != 1 {
			unauthorized()
			return
		}
		scheme, encoded, ok := strings.Cut(headers[0], " ")
		encoded = strings.TrimLeft(encoded, " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || encoded == "" || len(encoded) > 4096 || strings.IndexFunc(encoded, unicode.IsSpace) >= 0 || h.tokens == nil {
			unauthorized()
			return
		}
		userID, err := h.tokens.Verify(encoded)
		if err != nil || userID <= 0 {
			unauthorized()
			return
		}
		ctx := context.WithValue(r.Context(), userIDContextKey{}, userID)
		ctx = context.WithValue(ctx, loggerContextKey{}, requestLogger(r).With("user_id", userID))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
