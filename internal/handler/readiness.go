package handler

import (
	"context"
	"net/http"
	"time"
)

type DatabasePinger interface {
	Ping(context.Context) error
}

type Readiness struct {
	database DatabasePinger
	timeout  time.Duration
}

func NewReadiness(database DatabasePinger, timeout time.Duration) *Readiness {
	return &Readiness{database: database, timeout: timeout}
}

func (h *Readiness) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.database == nil {
		writeError(w, r, http.StatusServiceUnavailable, "not_ready", "Database unavailable", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	if err := h.database.Ping(ctx); err != nil {
		// Connection errors may include credentials; do not log their text.
		requestLogger(r).ErrorContext(ctx, "Database readiness check failed")
		writeError(w, r, http.StatusServiceUnavailable, "not_ready", "Database unavailable", nil)
		return
	}
	writeData(w, r, http.StatusOK, map[string]string{"status": "ready"})
}
