package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/kanatovs/taskflow-api/internal/config"
	"github.com/kanatovs/taskflow-api/internal/handler"
	"github.com/kanatovs/taskflow-api/internal/platform/database"
	"github.com/kanatovs/taskflow-api/internal/platform/httpserver"
)

func main() {
	os.Exit(run())
}

func run() int {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("Invalid configuration", "error", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := database.New(ctx, cfg.DatabaseURL, cfg.DatabaseTimeout)
	if err != nil {
		logger.Error("Database configuration failed", "error", err)
		return 1
	}
	defer pool.Close()

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler.NewRouter(logger, handler.NewReadiness(pool, cfg.DatabaseTimeout)),
		ReadHeaderTimeout: cfg.HTTPReadHeaderTimeout,
		ReadTimeout:       cfg.HTTPReadTimeout,
		WriteTimeout:      cfg.HTTPWriteTimeout,
		IdleTimeout:       cfg.HTTPIdleTimeout,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		logger.Error("HTTP listener failed", "error", err)
		return 1
	}
	if err := httpserver.Serve(ctx, server, listener, cfg.HTTPShutdownTimeout, logger); err != nil {
		logger.Error("HTTP server failed", "error", err)
		return 1
	}
	return 0
}
