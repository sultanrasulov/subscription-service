// Package postgres opens the database connection pool and applies the
// embedded schema migrations. It knows nothing about
// subscriptions; the adapters in internal/subscription use the pool it opens.
package postgres

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sultanrasulov/subscription-service/internal/platform/config"
)

// NewPool opens a connection pool and pings the database.
//
// The connect timeout bounds opening every connection, including the ones the
// pool opens later. It does not bound a query on an open connection, so the
// startup ping gets a deadline of its own: the context from main has none, and
// a server that accepts the connection and then stalls would hang the startup.
//
// Errors name the address, never the connection string, which holds the
// password.
func NewPool(ctx context.Context, cfg config.Postgres) (*pgxpool.Pool, error) {
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))

	pc, err := poolConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("configure postgres pool for %s: %w", addr, err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool for %s: %w", addr, err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres at %s: %w", addr, err)
	}
	return pool, nil
}

// poolConfig builds the pool configuration. The connect timeout is set as a
// duration on the connection config, not as connect_timeout in the connection
// string, which takes whole seconds. The other pool settings keep pgx's defaults
// until load shows a need.
func poolConfig(cfg config.Postgres) (*pgxpool.Config, error) {
	pc, err := pgxpool.ParseConfig(cfg.ConnString())
	if err != nil {
		return nil, err
	}
	pc.MaxConns = cfg.MaxConns
	pc.ConnConfig.ConnectTimeout = cfg.ConnectTimeout
	return pc, nil
}
