// Package pgtest starts a disposable PostgreSQL container for integration
// tests (design §11). It is reused by every package that tests against a real
// database.
//
// The image is read from compose.yaml, so the tests always run on the version
// the stack runs (design §10.5). The database is created with the C locale and
// UTF8 encoding, the least forgiving setup, so every integration test also
// shows that results do not depend on the locale (design §5.2).
package pgtest

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/sultanrasulov/subscription-service/internal/platform/config"
)

// Credentials of the test database. They never leave the test container.
const (
	database = "subscriptions"
	user     = "subscriptions"
	password = "subscriptions"
)

// imageLine matches the postgres service's image line in compose.yaml.
var imageLine = regexp.MustCompile(`(?m)^\s+image:\s*(postgres:\S+)\s*$`)

// Image returns the postgres service's image from compose.yaml at the module
// root. It walks up from the working directory to the directory that holds
// go.mod, so it works from any package's test.
//
// A line search is enough because the project owns the file's shape; it
// avoids a YAML dependency.
func Image() (string, error) {
	root, err := moduleRoot()
	if err != nil {
		return "", err
	}
	// A fixed name inside the module root, read through an FS rooted there.
	content, err := fs.ReadFile(os.DirFS(root), "compose.yaml")
	if err != nil {
		return "", fmt.Errorf("read compose.yaml in %s: %w", root, err)
	}
	m := imageLine.FindSubmatch(content)
	if m == nil {
		return "", fmt.Errorf("compose.yaml in %s has no `image: postgres:…` line", root)
	}
	return string(m[1]), nil
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("find the module root: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("find the module root: no go.mod above the working directory")
		}
		dir = parent
	}
}

// Start runs a PostgreSQL container and returns the configuration to reach it,
// and a function that stops the container and reports any error.
//
// The configuration is built from the container's host and mapped port, not
// from testcontainers' ready-made connection string, which does not escape the
// password (verified). config.Postgres.ConnString escapes it.
func Start(ctx context.Context) (config.Postgres, func(context.Context) error, error) {
	image, err := Image()
	if err != nil {
		return config.Postgres{}, nil, err
	}

	container, err := tcpostgres.Run(ctx, image,
		tcpostgres.WithDatabase(database),
		tcpostgres.WithUsername(user),
		tcpostgres.WithPassword(password),
		// C alone would give SQL_ASCII, where pg_c_utf8 does not exist (design §5.2).
		testcontainers.WithEnv(map[string]string{"POSTGRES_INITDB_ARGS": "--locale=C --encoding=UTF8"}),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		// Run returns the container even when it fails, for example on the
		// wait strategy, and it must still be removed: Ryuk, the reaper, may be
		// disabled in CI. TerminateContainer accepts a nil container.
		return config.Postgres{}, nil, errors.Join(fmt.Errorf("start postgres container: %w", err),
			testcontainers.TerminateContainer(container))
	}
	stop := func(ctx context.Context) error {
		return testcontainers.TerminateContainer(container, testcontainers.StopContext(ctx))
	}

	host, err := container.Host(ctx)
	if err != nil {
		return config.Postgres{}, nil, errors.Join(fmt.Errorf("container host: %w", err), stop(ctx))
	}
	mapped, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return config.Postgres{}, nil, errors.Join(fmt.Errorf("container port: %w", err), stop(ctx))
	}
	port, err := strconv.Atoi(mapped.Port())
	if err != nil {
		return config.Postgres{}, nil, errors.Join(fmt.Errorf("container port %q: %w", mapped.Port(), err), stop(ctx))
	}

	return config.Postgres{
		Host:           host,
		Port:           port,
		User:           user,
		Password:       password,
		Database:       database,
		SSLMode:        "disable",
		MaxConns:       5,
		ConnectTimeout: 5 * time.Second,
	}, stop, nil
}
