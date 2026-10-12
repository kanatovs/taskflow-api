package handler

import (
	"context"
	"crypto/rand"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

type loggerContextKey struct{}

func requestLogger(r *http.Request) *slog.Logger {
	if logger, ok := r.Context().Value(loggerContextKey{}).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}

func requestMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			requestID := rand.Text()
			logger := logger.With("request_id", requestID, "method", r.Method, "path", r.URL.Path)
			r = r.WithContext(context.WithValue(r.Context(), loggerContextKey{}, logger))
			w.Header().Set("X-Request-ID", requestID)
			writer := &responseWriter{ResponseWriter: w}
			aborted := false

			defer func() {
				status := writer.status
				if status == 0 && !aborted {
					status = http.StatusOK
				}
				logger.InfoContext(r.Context(), "HTTP request completed",
					"status", status,
					"response_bytes", writer.bytes,
					"duration_ms", float64(time.Since(started))/float64(time.Millisecond),
					"aborted", aborted)
			}()
			defer func() {
				if value := recover(); value != nil {
					if value == http.ErrAbortHandler {
						aborted = true
						panic(value)
					}
					// Panic values can contain credentials; only log the call stack.
					logger.ErrorContext(r.Context(), "HTTP handler panicked", "stack", string(debug.Stack()))
					if writer.status != 0 {
						// net/http must abort a response that has already started.
						aborted = true
						panic(http.ErrAbortHandler)
					}
					writer.Header().Del("Content-Length")
					writer.Header().Del("Content-Encoding")
					writeError(writer, r, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
				}
			}()

			next.ServeHTTP(writer, r)
		})
	}
}

type responseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *responseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.ResponseWriter.WriteHeader(status)
	w.status = status
}

func (w *responseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(body)
	w.bytes += n
	return n, err
}

// FlushError lets http.ResponseController flush without bypassing status tracking.
func (w *responseWriter) FlushError() error {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
