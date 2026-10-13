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
}

type AuthHandler struct{ service AuthService }

func NewAuthHandler(service AuthService) *AuthHandler { return &AuthHandler{service: service} }

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
	case errors.Is(err, domain.ErrInvalidCredentials):
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "Invalid email or password", nil)
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
