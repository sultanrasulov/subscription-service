package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sultanrasulov/subscription-service/internal/platform/config"
	"github.com/sultanrasulov/subscription-service/internal/platform/postgres/pgtest"
	"github.com/sultanrasulov/subscription-service/migrations"
)

// server reaches the package's one test container; zero under -short.
var server config.Postgres

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}

	ctx := context.Background()
	cfg, stop, err := pgtest.Start(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	server = cfg

	code := m.Run()
	if err := stop(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "stop postgres container:", err)
	}
	os.Exit(code)
}

// databases numbers the databases freshDB creates.
var databases atomic.Int64

// freshDB creates an empty database in the test container, with the given
// encoding, and drops it when the test ends. Each test gets its own database,
// so tests stay independent and can run in parallel.
func freshDB(t *testing.T, encoding string) config.Postgres {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test: needs Docker")
	}

	ctx := context.Background()
	admin, err := pgxpool.New(ctx, server.ConnString())
	if err != nil {
		t.Fatalf("connect as admin: %v", err)
	}
	t.Cleanup(admin.Close)

	name := fmt.Sprintf("test_%d", databases.Add(1))
	if _, err := admin.Exec(ctx, fmt.Sprintf(
		"CREATE DATABASE %s TEMPLATE template0 ENCODING '%s' LOCALE 'C'", name, encoding)); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)"); err != nil {
			t.Errorf("drop database %s: %v", name, err)
		}
	})

	cfg := server
	cfg.Database = name
	return cfg
}

// newPool opens a pool on cfg and closes it when the test ends.
func newPool(t *testing.T, cfg config.Postgres) *pgxpool.Pool {
	t.Helper()
	pool, err := NewPool(t.Context(), cfg)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// --- Without Docker ---

func TestPoolConfig_AppliesSettings(t *testing.T) {
	t.Parallel()

	cfg := config.Postgres{
		Host: "db", Port: 5432, User: "app", Password: "secret", Database: "subscriptions",
		SSLMode: "disable", MaxConns: 7, ConnectTimeout: 1500 * time.Millisecond,
	}
	pc, err := poolConfig(cfg)
	if err != nil {
		t.Fatalf("poolConfig() error = %v", err)
	}
	if pc.MaxConns != 7 {
		t.Errorf("MaxConns = %d, want 7", pc.MaxConns)
	}
	// The timeout bounds every new connection the pool opens, later ones included.
	if pc.ConnConfig.ConnectTimeout != 1500*time.Millisecond {
		t.Errorf("ConnectTimeout = %v, want 1.5s", pc.ConnConfig.ConnectTimeout)
	}
}

func TestNewPool_FailsOnClosedPort(t *testing.T) {
	t.Parallel()

	cfg := config.Postgres{
		Host: "127.0.0.1", Port: 1, User: "app", Password: "supersecret", Database: "subscriptions",
		SSLMode: "disable", MaxConns: 1, ConnectTimeout: time.Second,
	}
	_, err := NewPool(t.Context(), cfg)
	if err == nil {
		t.Fatal("NewPool() error = nil, want an error")
	}
	if strings.Contains(err.Error(), "supersecret") {
		t.Errorf("error leaks the password: %v", err)
	}
	if !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Errorf("error does not name the address: %v", err)
	}
}

// A server that accepts the TCP connection and never answers must not hang
// the startup: the connect timeout bounds the handshake, and the startup ping
// has a deadline of its own.
func TestNewPool_FailsWithinTimeoutOnSilentServer(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = conn.Close() })
		}
	}()

	cfg := config.Postgres{
		Host: "127.0.0.1", Port: listenerPort(t, ln), User: "app", Password: "secret", Database: "subscriptions",
		SSLMode: "disable", MaxConns: 1, ConnectTimeout: 500 * time.Millisecond,
	}
	start := time.Now()
	_, err = NewPool(t.Context(), cfg)
	if err == nil {
		t.Fatal("NewPool() error = nil, want an error")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("NewPool() took %v, want it bounded by the 500ms timeout", elapsed)
	}
}

// A server that completes the handshake and then never answers a query must
// not hang the startup either. The connect timeout does not cover a query on
// an open connection, so only the ping's own deadline stops it.
func TestNewPool_FailsWithinTimeoutOnStalledQuery(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = conn.Close() })
			go acceptThenStall(conn)
		}
	}()

	cfg := config.Postgres{
		Host: "127.0.0.1", Port: listenerPort(t, ln), User: "app", Password: "secret", Database: "subscriptions",
		SSLMode: "disable", MaxConns: 1, ConnectTimeout: 500 * time.Millisecond,
	}
	start := time.Now()
	_, err = NewPool(t.Context(), cfg)
	if err == nil {
		t.Fatal("NewPool() error = nil, want an error")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("NewPool() took %v, want it bounded by the 500ms timeout", elapsed)
	}
}

// acceptThenStall plays a Postgres server that lets every client in without a
// password and then reads queries without ever answering them.
//
// Like a real server, it closes the connection when it is done with it: after
// Terminate, after a cancel request, or after a message it cannot read. pgx
// waits for that close, for up to 15 seconds, both on the connection it
// terminates and on the one that carries the cancel request.
func acceptThenStall(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	backend := pgproto3.NewBackend(conn, conn)
	msg, err := backend.ReceiveStartupMessage()
	if err != nil {
		return
	}
	if _, ok := msg.(*pgproto3.StartupMessage); !ok {
		return // a cancel request or an SSL request
	}
	backend.Send(&pgproto3.AuthenticationOk{})
	backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	if err := backend.Flush(); err != nil {
		return
	}
	for {
		msg, err := backend.Receive()
		if err != nil {
			return
		}
		if _, ok := msg.(*pgproto3.Terminate); ok {
			return
		}
	}
}

// listenerPort returns the TCP port ln listens on.
func listenerPort(t *testing.T, ln net.Listener) int {
	t.Helper()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address %v is not TCP", ln.Addr())
	}
	return addr.Port
}

// --- With Docker ---

func TestNewPool_ConnectsAndPings(t *testing.T) {
	t.Parallel()

	pool := newPool(t, freshDB(t, "UTF8"))
	if err := pool.Ping(t.Context()); err != nil {
		t.Errorf("Ping() error = %v", err)
	}
	if pool.Config().MaxConns != server.MaxConns {
		t.Errorf("MaxConns = %d, want %d", pool.Config().MaxConns, server.MaxConns)
	}
}

func TestNewPool_FailsOnWrongPassword(t *testing.T) {
	t.Parallel()

	cfg := freshDB(t, "UTF8")
	cfg.Password = "wrong-password"
	_, err := NewPool(t.Context(), cfg)
	if err == nil {
		t.Fatal("NewPool() error = nil, want an error")
	}
	if strings.Contains(err.Error(), "wrong-password") {
		t.Errorf("error leaks the password: %v", err)
	}
}

func TestMigrate_AppliesSchemaAndIsIdempotent(t *testing.T) {
	t.Parallel()

	pool := newPool(t, freshDB(t, "UTF8"))
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))

	if err := Migrate(t.Context(), pool, migrations.FS, log); err != nil {
		t.Fatalf("first Migrate() error = %v", err)
	}
	if err := Migrate(t.Context(), pool, migrations.FS, log); err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}

	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 log lines, got:\n%s", logs.String())
	}
	for i, want := range [][2]float64{{0, 1}, {1, 1}} {
		var line map[string]any
		if err := json.Unmarshal([]byte(lines[i]), &line); err != nil {
			t.Fatalf("log line %d: %v", i, err)
		}
		if line["msg"] != "migrations applied" || line["from"] != want[0] || line["to"] != want[1] {
			t.Errorf("log line %d = %v, want migrations applied from %v to %v", i, line, want[0], want[1])
		}
	}

	var collation string
	if err := pool.QueryRow(t.Context(), `SELECT collation_name FROM information_schema.columns
		WHERE table_name = 'subscriptions' AND column_name = 'service_name'`).Scan(&collation); err != nil {
		t.Fatalf("read collation: %v", err)
	}
	if collation != "pg_c_utf8" {
		t.Errorf("service_name collation = %q, want pg_c_utf8", collation)
	}

	for _, index := range []string{"subscriptions_user_id_idx", "subscriptions_service_name_lower_idx"} {
		var found bool
		if err := pool.QueryRow(t.Context(),
			`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE tablename = 'subscriptions' AND indexname = $1)`,
			index).Scan(&found); err != nil {
			t.Fatalf("read index %s: %v", index, err)
		}
		if !found {
			t.Errorf("index %s is missing", index)
		}
	}
}

func TestMigrate_DownThenUpAgain(t *testing.T) {
	t.Parallel()

	pool := newPool(t, freshDB(t, "UTF8"))
	if err := Migrate(t.Context(), pool, migrations.FS, discardLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	m, closeAll, err := newMigrator(pool, migrations.FS)
	if err != nil {
		t.Fatalf("newMigrator() error = %v", err)
	}
	if err := m.Down(); err != nil {
		t.Fatalf("Down() error = %v", err)
	}
	if err := closeAll(); err != nil {
		t.Fatalf("close the migrator: %v", err)
	}
	if tableExists(t, pool) {
		t.Fatal("subscriptions still exists after Down()")
	}

	if err := Migrate(t.Context(), pool, migrations.FS, discardLogger()); err != nil {
		t.Fatalf("Migrate() after Down() error = %v", err)
	}
	if !tableExists(t, pool) {
		t.Fatal("subscriptions is missing after Migrate()")
	}
}

func tableExists(t *testing.T, pool *pgxpool.Pool) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(t.Context(), `SELECT to_regclass('subscriptions') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatalf("check the table: %v", err)
	}
	return exists
}

// An unclosed migrator keeps one pool connection checked out, and
// pool.Close() then blocks forever (verified).
func TestMigrate_LeavesPoolClosable(t *testing.T) {
	t.Parallel()

	pool, err := NewPool(t.Context(), freshDB(t, "UTF8"))
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	if err := Migrate(t.Context(), pool, migrations.FS, discardLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	closed := make(chan struct{})
	go func() {
		pool.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("pool.Close() blocked after Migrate(): a connection is still checked out")
	}
}

// pg_c_utf8 exists only in UTF8 databases, so the first migration fails on any
// other encoding, by design.
func TestMigrate_FailsOnNonUTF8Database(t *testing.T) {
	t.Parallel()

	pool := newPool(t, freshDB(t, "SQL_ASCII"))
	err := Migrate(t.Context(), pool, migrations.FS, discardLogger())
	if err == nil {
		t.Fatal("Migrate() error = nil, want an error")
	}
	if !strings.Contains(err.Error(), `collation "pg_c_utf8" for encoding "SQL_ASCII" does not exist`) {
		t.Errorf("Migrate() error = %v, want the pg_c_utf8 collation error", err)
	}

	// The failed migration leaves version 1 dirty; the next start must refuse
	// and point to the manual recovery.
	err = Migrate(t.Context(), pool, migrations.FS, discardLogger())
	if err == nil || !strings.Contains(err.Error(), "dirty") || !strings.Contains(err.Error(), "migrate force") {
		t.Errorf("second Migrate() error = %v, want the dirty-version error", err)
	}
}

func TestSchema_RejectsInvalidRows(t *testing.T) {
	t.Parallel()

	pool := newPool(t, freshDB(t, "UTF8"))
	if err := Migrate(t.Context(), pool, migrations.FS, discardLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	tests := []struct {
		name       string
		service    string
		price      int
		start, end any
		constraint string
	}{
		{"negative price", "Netflix", -1, "2025-07-01", nil, "subscriptions_price_non_negative"},
		{"empty name", "", 400, "2025-07-01", nil, "subscriptions_service_name_length"},
		{"name of 101 runes", strings.Repeat("я", 101), 400, "2025-07-01", nil, "subscriptions_service_name_length"},
		{"start not on the 1st", "Netflix", 400, "2025-07-15", nil, "subscriptions_start_date_month"},
		{"start before 2000", "Netflix", 400, "1999-12-01", nil, "subscriptions_start_date_month"},
		{"end not on the 1st", "Netflix", 400, "2025-07-01", "2025-08-15", "subscriptions_end_date_month"},
		{"end after 2100", "Netflix", 400, "2025-07-01", "2101-01-01", "subscriptions_end_date_month"},
		{"end before start", "Netflix", 400, "2025-07-01", "2025-06-01", "subscriptions_end_not_before_start"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pool.Exec(t.Context(), `INSERT INTO subscriptions
				(id, user_id, service_name, price, start_date, end_date)
				VALUES (gen_random_uuid(), gen_random_uuid(), $1, $2, $3, $4)`,
				tc.service, tc.price, tc.start, tc.end)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) {
				t.Fatalf("insert error = %v, want a check violation", err)
			}
			if pgErr.Code != "23514" || pgErr.ConstraintName != tc.constraint {
				t.Errorf("violation = %s %s, want 23514 %s", pgErr.Code, pgErr.ConstraintName, tc.constraint)
			}
		})
	}

	t.Run("boundary row is accepted", func(t *testing.T) {
		if _, err := pool.Exec(t.Context(), `INSERT INTO subscriptions
			(id, user_id, service_name, price, start_date, end_date)
			VALUES (gen_random_uuid(), gen_random_uuid(), $1, 0, '2000-01-01', '2100-12-01')`,
			strings.Repeat("я", 100)); err != nil {
			t.Errorf("insert error = %v, want the row accepted", err)
		}
	})
}
