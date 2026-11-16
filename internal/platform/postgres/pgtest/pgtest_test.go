package pgtest_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/sultanrasulov/subscription-service/internal/platform/config"
	"github.com/sultanrasulov/subscription-service/internal/platform/postgres/pgtest"
)

// server is the test container, started once for the package.
var server config.Postgres

func TestMain(m *testing.M) {
	os.Exit(pgtest.Main(m, &server))
}

// The tests must run on the image the stack runs, read from compose.yaml, the
// single source of that version.
func TestImage_ReadsComposeFile(t *testing.T) {
	t.Parallel()

	img, err := pgtest.Image()
	if err != nil {
		t.Fatalf("Image() error = %v", err)
	}
	if !strings.HasPrefix(img, "postgres:17.") {
		t.Errorf("Image() = %q, want a postgres:17.x image", img)
	}
}

func TestNewDatabase_CreatesThenDrops(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test: needs Docker")
	}

	for _, encoding := range []string{"UTF8", "SQL_ASCII"} {
		var name string
		// A subtest, so its cleanup, which drops the database, has run by the
		// time the drop is checked below.
		t.Run(encoding, func(t *testing.T) {
			cfg := pgtest.NewDatabase(t, server, encoding)
			name = cfg.Database
			if name == server.Database {
				t.Fatalf("NewDatabase() returned the server's own database %q", name)
			}

			conn := connect(t, cfg)
			var gotEncoding, collate string
			if err := conn.QueryRow(t.Context(), `SELECT pg_encoding_to_char(encoding), datcollate
				FROM pg_database WHERE datname = current_database()`).Scan(&gotEncoding, &collate); err != nil {
				t.Fatalf("read the database settings: %v", err)
			}
			if gotEncoding != encoding || collate != "C" {
				t.Errorf("database settings = %s, %s; want %s, C", gotEncoding, collate, encoding)
			}
		})

		var exists bool
		conn := connect(t, server)
		if err := conn.QueryRow(t.Context(),
			`SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, name).Scan(&exists); err != nil {
			t.Fatalf("look for the database: %v", err)
		}
		if exists {
			t.Errorf("database %q still exists after its test ended", name)
		}
	}
}

func connect(t *testing.T, cfg config.Postgres) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(t.Context(), cfg.ConnString())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		// The test's context is canceled before cleanup runs.
		if err := conn.Close(context.Background()); err != nil {
			t.Errorf("close the connection: %v", err)
		}
	})
	return conn
}
