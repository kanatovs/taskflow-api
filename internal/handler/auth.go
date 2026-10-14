package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/kanatovs/taskflow-api/internal/domain"
	"github.com/kanatovs/taskflow-api/internal/service"
)

type AuthService interface {
	Register(context.Context, service.RegisterInput) (domain.User, error)
	Login(context.Context, service.LoginInput) (domain.AccessToken, error)
	Me(context.Context, int64) (domain.User, error)
}

type TokenVerifier interface{ Verify(string) (int64, error) }

type AuthHandler struct {
	service AuthService
	tokens  TokenVerifier
}

func NewAuthHandler(service AuthService, tokens TokenVerifier) *AuthHandler {
	return &AuthHandler{service: service, tokens: tokens}
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(userIDContextKey{}).(int64)
	if !ok {
		writeServiceError(w, r, "current user", domain.ErrUnauthenticated)
		return
	}
	user, err := h.service.Me(r.Context(), userID)
	if err != nil {
		writeServiceError(w, r, "current user", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeData(w, r, http.StatusOK, user)
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var input service.LoginInput
	if !decodeRequest(w, r, &input) {
		return
	}
	access, err := h.service.Login(r.Context(), input)
	if err != nil {
		writeServiceError(w, r, "login user", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeData(w, r, http.StatusOK, access)
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var input service.RegisterInput
	if !decodeRequest(w, r, &input) {
		return
	}
	user, err := h.service.Register(r.Context(), input)
	if err != nil {
		writeServiceError(w, r, "register user", err)
		return
	}
	writeData(w, r, http.StatusCreated, user)
}

func decodeRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", "Request body must contain valid JSON", nil)
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeError(w, r, http.StatusBadRequest, "validation_error", "Request body must contain one JSON object", nil)
		return false
	}
	return true
}

func writeServiceError(w http.ResponseWriter, r *http.Request, operation string, err error) {
	var validation *domain.ValidationError
	switch {
	case errors.As(err, &validation):
		details := make(map[string]any, len(validation.Fields))
		for field, message := range validation.Fields {
			details[field] = message
		}
		writeError(w, r, http.StatusBadRequest, "validation_error", "Request validation failed", details)
	case errors.Is(err, domain.ErrEmailExists):
		writeError(w, r, http.StatusConflict, "conflict", "Email already registered", nil)
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "Resource not found", nil)
	case errors.Is(err, domain.ErrForbidden):
		writeError(w, r, http.StatusForbidden, "forbidden", "Permission denied", nil)
	case errors.Is(err, domain.ErrConflict):
		writeError(w, r, http.StatusConflict, "conflict", "Resource conflict", nil)
	case errors.Is(err, domain.ErrInvalidCredentials):
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "Invalid email or password", nil)
	case errors.Is(err, domain.ErrUnauthenticated):
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "Authentication required", nil)
	default:
		kind := "unexpected"
		if errors.Is(err, context.DeadlineExceeded) {
			kind = "deadline_exceeded"
		}
		if errors.Is(err, context.Canceled) {
			kind = "cancelled"
		}
		// Raw repository errors can include connection secrets or a failed row.
		requestLogger(r).ErrorContext(r.Context(), "Service operation failed", "operation", operation, "error_kind", kind)
		writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
	}
}
