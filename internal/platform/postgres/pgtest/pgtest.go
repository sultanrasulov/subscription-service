// Package pgtest starts a disposable PostgreSQL container for integration
// tests. It is reused by every package that tests against a real
// database.
//
// The image is read from compose.yaml, so the tests always run on the version
// the stack runs. The database is created with the C locale and
// UTF8 encoding, the least forgiving setup, so every integration test also
// shows that results do not depend on the locale.
package pgtest

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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
		// C alone would give SQL_ASCII, where pg_c_utf8 does not exist.
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

// Main runs a test package against one container. It starts the container,
// stores its configuration in *server, runs the tests and stops the container,
// and returns the exit code; a package's TestMain is then
// os.Exit(pgtest.Main(m, &server)). Under -short it only runs the tests, which
// skip themselves.
func Main(m *testing.M, server *config.Postgres) int {
	// testing.Short reads the flags, and TestMain runs before m.Run parses them.
	flag.Parse()
	if testing.Short() {
		return m.Run()
	}

	ctx := context.Background()
	cfg, stop, err := Start(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	*server = cfg

	code := m.Run()
	if err := stop(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "stop postgres container:", err)
	}
	return code
}

// databases numbers the databases NewDatabase creates, so their names are
// unique within a test binary.
var databases atomic.Int64

// NewDatabase creates an empty database on server, with the given encoding
// and the C locale, and drops it when the test ends. Each test gets its own
// database, so tests stay independent and can run in parallel. It skips the
// test under -short, because it needs Docker.
//
// It only creates the database. Applying the migrations is the caller's job:
// the package that applies them imports this one in its own tests, so doing it
// here would be an import cycle.
func NewDatabase(t testing.TB, server config.Postgres, encoding string) config.Postgres {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test: needs Docker")
	}

	// Not t.Context(): it is canceled before cleanup runs, and the drop below
	// runs in cleanup.
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, server.ConnString())
	if err != nil {
		t.Fatalf("connect as admin: %v", err)
	}
	t.Cleanup(admin.Close)

	// CREATE DATABASE takes no bind parameters, so the name is quoted as an
	// identifier and the encoding as a string literal.
	name := fmt.Sprintf("test_%d", databases.Add(1))
	ident := pgx.Identifier{name}.Sanitize()
	literal := "'" + strings.ReplaceAll(encoding, "'", "''") + "'"
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+ident+" TEMPLATE template0 ENCODING "+literal+" LOCALE 'C'"); err != nil {
		t.Fatalf("create database %s: %v", name, err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(ctx, "DROP DATABASE "+ident+" WITH (FORCE)"); err != nil {
			t.Errorf("drop database %s: %v", name, err)
		}
	})

	cfg := server
	cfg.Database = name
	return cfg
}
