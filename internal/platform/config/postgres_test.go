package config_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/sultanrasulov/subscription-service/internal/platform/config"
)

func TestLoad_PostgresDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(base())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := config.Postgres{
		Host:           "db",
		Port:           5432,
		User:           "app",
		Password:       "secret",
		Database:       "subscriptions",
		SSLMode:        "disable",
		MaxConns:       10,
		ConnectTimeout: 5 * time.Second,
	}
	if cfg.Postgres != want {
		t.Errorf("Postgres = %+v, want %+v", cfg.Postgres, want)
	}
	if cfg.Postgres.Password.Reveal() != "secret" {
		t.Errorf("Password.Reveal() = %q, want %q", cfg.Postgres.Password.Reveal(), "secret")
	}
}

func TestLoad_PostgresValues(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(with(
		"POSTGRES_PORT", "6543",
		"POSTGRES_SSLMODE", "verify-full",
		"POSTGRES_MAX_CONNS", "25",
		"POSTGRES_CONNECT_TIMEOUT", "1500ms",
	))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Postgres.Port != 6543 || cfg.Postgres.SSLMode != "verify-full" || cfg.Postgres.MaxConns != 25 {
		t.Errorf("Postgres = %+v", cfg.Postgres)
	}
	// A sub-second value survives: the timeout reaches pgx as a duration,
	// not as connect_timeout in the connection string, which takes whole seconds.
	if cfg.Postgres.ConnectTimeout != 1500*time.Millisecond {
		t.Errorf("ConnectTimeout = %v, want 1.5s", cfg.Postgres.ConnectTimeout)
	}
}

func TestLoad_RejectsInvalidPostgres(t *testing.T) {
	t.Parallel()

	checkRejected(t, []invalid{
		{"POSTGRES_PORT", "x", `"x"`},
		{"POSTGRES_PORT", "0", "0 must be between 1 and 65535"},
		{"POSTGRES_PORT", "65536", "65536 must be between 1 and 65535"},
		{"POSTGRES_MAX_CONNS", "0", "0 must be positive"},
		{"POSTGRES_CONNECT_TIMEOUT", "abc", `"abc"`},
		{"POSTGRES_CONNECT_TIMEOUT", "0s", "0s must be positive"},
		{"POSTGRES_CONNECT_TIMEOUT", "-1s", "-1s must be positive"},
		{"POSTGRES_SSLMODE", "on", `"on" must be one of`},
		{"POSTGRES_HOST", "", "must not be empty"},
		{"POSTGRES_USER", "", "must not be empty"},
		{"POSTGRES_PASSWORD", "", "must not be empty"},
		{"POSTGRES_DB", "", "must not be empty"},
	})
}

func TestPostgres_ConnStringEscapesReservedCharacters(t *testing.T) {
	t.Parallel()

	p := config.Postgres{
		Host:     "db",
		Port:     5432,
		User:     "us@r:/?#%",
		Password: "p@ss:/w?rd#%",
		Database: "subscriptions",
		SSLMode:  "disable",
		MaxConns: 10,
	}

	pc, err := pgconn.ParseConfig(p.ConnString())
	if err != nil {
		t.Fatalf("pgconn.ParseConfig() error = %v", err)
	}
	if pc.Host != "db" || pc.Port != 5432 || pc.Database != "subscriptions" {
		t.Errorf("parsed = host %q port %d database %q", pc.Host, pc.Port, pc.Database)
	}
	if pc.User != "us@r:/?#%" {
		t.Errorf("User = %q, want %q", pc.User, "us@r:/?#%")
	}
	if pc.Password != "p@ss:/w?rd#%" {
		t.Errorf("Password = %q, want %q", pc.Password, "p@ss:/w?rd#%")
	}
	if pc.TLSConfig != nil {
		t.Error("TLSConfig != nil, want nil for sslmode=disable")
	}
}

func TestPostgres_PrintingNeverShowsThePassword(t *testing.T) {
	t.Parallel()

	p := config.Postgres{Host: "db", Port: 5432, User: "app", Password: "supersecret", Database: "subscriptions"}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		if out := fmt.Sprintf(verb, p); strings.Contains(out, "supersecret") {
			t.Errorf("fmt %s leaks the password: %s", verb, out)
		}
	}
}
