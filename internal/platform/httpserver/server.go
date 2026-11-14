package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/sultanrasulov/subscription-service/internal/platform/config"
)

// NewServer returns an HTTP server for h with the configured timeouts
// (design §7). net/http's own messages, such as TLS handshake errors, go
// through log at WARN.
//
// Request contexts do not derive from the service's context: a shutdown must
// let in-flight requests finish, not cancel them. Each request is bounded by
// its own deadline instead (design §4.4).
func NewServer(cfg config.HTTP, h http.Handler, log *slog.Logger) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
}

// Serve serves srv on ln until ctx is canceled, then shuts down gracefully
// (design §3.4): it stops accepting connections and waits up to
// shutdownTimeout for in-flight requests.
//
// It returns nil after a clean shutdown. If the shutdown overruns, the server
// is closed, cutting the remaining requests off, and the error says so. If
// serving fails on its own, Serve returns that error.
func Serve(ctx context.Context, srv *http.Server, ln net.Listener, shutdownTimeout time.Duration) error {
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()

	select {
	case err := <-served:
		return fmt.Errorf("serve http: %w", err)
	case <-ctx.Done():
	}

	// ctx is already canceled; the shutdown needs a deadline of its own.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return errors.Join(fmt.Errorf("shut down http server: %w", err), srv.Close())
	}

	// Serve returns http.ErrServerClosed as soon as Shutdown starts.
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve http: %w", err)
	}
	return nil
}
