// Command subscription-service runs the subscription aggregation REST service.
//
// main is the composition root (design §3.3): it owns the process (its
// environment, its signals and its exit code) and wires the packages together
// by hand.
//
//	@title			Subscription Service API
//	@version		1.0
//	@description	Aggregates users' online subscriptions and reports their total cost.
//	@BasePath		/api/v1
package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/sultanrasulov/subscription-service/docs"
	"github.com/sultanrasulov/subscription-service/internal/platform/config"
	"github.com/sultanrasulov/subscription-service/internal/platform/httpserver"
	"github.com/sultanrasulov/subscription-service/internal/platform/logger"
	"github.com/sultanrasulov/subscription-service/internal/platform/postgres"
	"github.com/sultanrasulov/subscription-service/migrations"
)

// main only exits: os.Exit skips deferred calls, so everything that defers
// lives in code, which returns the exit code.
func main() {
	os.Exit(code(environ(), os.Stdout))
}

// code runs the service and returns the process exit code.
//
// A fatal error is logged here, once (design §8). The configuration decides
// the log format, so an invalid configuration is logged with the default
// logger (JSON, decision #65); every later error uses the configured one.
func code(environ map[string]string, stdout io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load(environ)
	if err != nil {
		logger.New(stdout, config.Log{}).ErrorContext(ctx, "invalid configuration", "error", err)
		return 1
	}
	log := logger.New(stdout, cfg.Log)
	if err := run(ctx, cfg, log); err != nil {
		log.ErrorContext(ctx, "service failed", "error", err)
		return 1
	}
	return 0
}

// run starts the service and serves until ctx is canceled, in the order of
// design §3.3–§3.4. It returns errors with context and never logs them.
func run(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	pool, err := postgres.NewPool(ctx, cfg.Postgres)
	if err != nil {
		return err
	}
	// Deferred first, so it runs last: the pool closes after the server.
	defer pool.Close()

	if err := postgres.Migrate(ctx, pool, migrations.FS, log); err != nil {
		return err
	}

	spec, err := fs.Sub(docs.Swagger, "swagger")
	if err != nil {
		return fmt.Errorf("open the swagger directory: %w", err)
	}
	handler := httpserver.NewHandler(httpserver.Options{
		Logger:         log,
		Ready:          pool.Ping,
		Swagger:        spec,
		RequestTimeout: cfg.HTTP.RequestTimeout,
	})

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.HTTP.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.HTTP.Addr, err)
	}
	log.InfoContext(ctx, "server started", "addr", ln.Addr().String())

	// "shutting down" marks the moment the signal arrives; Serve returns only
	// once the in-flight requests have finished.
	noticed := make(chan struct{})
	stopNotice := context.AfterFunc(ctx, func() {
		log.InfoContext(ctx, "shutting down")
		close(noticed)
	})

	err = httpserver.Serve(ctx, httpserver.NewServer(cfg.HTTP, handler, log), ln, cfg.HTTP.ShutdownTimeout)
	// If the notice has started, let it finish, so the lines stay in order.
	if !stopNotice() {
		<-noticed
	}
	if err != nil {
		return err
	}
	log.InfoContext(ctx, "server stopped")
	return nil
}

// environ returns the process environment as the map config.Load takes. It
// lives here, with the process, so the configuration library stays inside
// internal/platform/config. Only the first "=" separates a name from its
// value.
func environ() map[string]string {
	vars := make(map[string]string)
	for _, kv := range os.Environ() {
		if name, value, ok := strings.Cut(kv, "="); ok {
			vars[name] = value
		}
	}
	return vars
}
