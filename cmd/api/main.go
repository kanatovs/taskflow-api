package main

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/kanatovs/taskflow-api/internal/handler"
)

func main() {
	server := &http.Server{
		Addr:              ":8080",
		Handler:           handler.NewRouter(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("HTTP server failed", "error", err)
		os.Exit(1)
	}
}
