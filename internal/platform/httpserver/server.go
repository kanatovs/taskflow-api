package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Serve owns the listener and waits for active requests during shutdown.
func Serve(ctx context.Context, server *http.Server, listener net.Listener, shutdownTimeout time.Duration, logger *slog.Logger) error {
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()
	logger.Info("HTTP server started", "addr", listener.Addr().String())

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			if closeErr := server.Close(); closeErr != nil {
				return errors.Join(fmt.Errorf("serve HTTP: %w", err), fmt.Errorf("close HTTP server: %w", closeErr))
			}
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	case <-ctx.Done():
	}

	logger.Info("HTTP server shutting down", "timeout", shutdownTimeout.String())
	// The signal context is already canceled, so draining needs a fresh deadline.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	shutdownErr := server.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		closeErr := server.Close()
		<-serveErr
		if closeErr != nil {
			return errors.Join(fmt.Errorf("shutdown HTTP server: %w", shutdownErr), fmt.Errorf("close HTTP server: %w", closeErr))
		}
		return fmt.Errorf("shutdown HTTP server: %w", shutdownErr)
	}
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP: %w", err)
	}
	logger.Info("HTTP server stopped")
	return nil
}
