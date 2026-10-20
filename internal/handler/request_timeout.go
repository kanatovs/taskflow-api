package handler

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// requestTimeout gives all context-aware work in a request one shared budget.
// It runs synchronously: handlers must cooperate with context cancellation.
func requestTimeout(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			r = r.WithContext(ctx)
			writer := &timeoutResponseWriter{responseWriter: &responseWriter{ResponseWriter: w}, request: r}
			next.ServeHTTP(writer, r)
			// A handler may return without writing after noticing cancellation.
			writer.expired()
		})
	}
}

type timeoutResponseWriter struct {
	*responseWriter
	request  *http.Request
	timedOut bool
}

func (w *timeoutResponseWriter) WriteHeader(status int) {
	if !w.expired() {
		w.responseWriter.WriteHeader(status)
	}
}

func (w *timeoutResponseWriter) Write(body []byte) (int, error) {
	if w.expired() {
		return 0, http.ErrHandlerTimeout
	}
	return w.responseWriter.Write(body)
}

func (w *timeoutResponseWriter) FlushError() error {
	if w.expired() {
		return http.ErrHandlerTimeout
	}
	return w.responseWriter.FlushError()
}

func (w *timeoutResponseWriter) expired() bool {
	if w.timedOut {
		return true
	}
	if !errors.Is(w.request.Context().Err(), context.DeadlineExceeded) {
		return false
	}
	if w.status != 0 {
		// A second JSON response would corrupt an already started response.
		panic(http.ErrAbortHandler)
	}

	w.timedOut = true
	requestID := w.Header().Get("X-Request-ID")
	clear(w.Header())
	if requestID != "" {
		w.Header().Set("X-Request-ID", requestID)
	}
	w.Header().Set("Cache-Control", "no-store")
	requestLogger(w.request).WarnContext(w.request.Context(), "HTTP request deadline exceeded")
	// Bypass our own methods to avoid recursively checking the expired context.
	writeError(w.responseWriter, w.request, http.StatusServiceUnavailable,
		"request_timeout", "Request processing timed out", nil)
	return true
}
