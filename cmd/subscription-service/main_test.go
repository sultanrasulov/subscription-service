package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sultanrasulov/subscription-service/internal/platform/config"
	"github.com/sultanrasulov/subscription-service/internal/platform/logger"
	"github.com/sultanrasulov/subscription-service/internal/platform/postgres/pgtest"
)

// --- environ ---

// A value may itself contain "=", for example a base64 password; only the
// first "=" separates the name.
func TestEnviron_SplitsOnTheFirstEquals(t *testing.T) {
	t.Setenv("SUBSCRIPTION_TEST_VAR", "a=b=c")

	if got := environ()["SUBSCRIPTION_TEST_VAR"]; got != "a=b=c" {
		t.Errorf("environ()[SUBSCRIPTION_TEST_VAR] = %q, want %q", got, "a=b=c")
	}
}

// --- code: the fatal error is logged exactly once, in the right format ---

func TestCode_InvalidConfigurationLogsOnceAsJSON(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	// LOG_FORMAT=text cannot apply: the configuration itself is broken.
	exit := code(map[string]string{"LOG_FORMAT": "text", "LOG_LEVEL": "verbose"}, &out)

	if exit != 1 {
		t.Errorf("exit code = %d, want 1", exit)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("want exactly one line, got %d:\n%s", len(lines), out.String())
	}
	var line map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &line); err != nil {
		t.Fatalf("the line is not JSON: %v\n%s", err, lines[0])
	}
	if line["level"] != "ERROR" || line["msg"] != "invalid configuration" {
		t.Errorf("line = %v, want ERROR invalid configuration", line)
	}
	if errText, _ := line["error"].(string); !strings.Contains(errText, "LOG_LEVEL") || !strings.Contains(errText, "POSTGRES_HOST") {
		t.Errorf("error = %q, want every configuration problem named", errText)
	}
}

func TestCode_StartupFailureLogsOnceInTheConfiguredFormat(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	exit := code(map[string]string{
		"LOG_FORMAT":               "text",
		"POSTGRES_HOST":            "127.0.0.1",
		"POSTGRES_PORT":            "1", // nothing listens there
		"POSTGRES_USER":            "app",
		"POSTGRES_PASSWORD":        "supersecret",
		"POSTGRES_DB":              "subscriptions",
		"POSTGRES_CONNECT_TIMEOUT": "1s",
	}, &out)

	if exit != 1 {
		t.Errorf("exit code = %d, want 1", exit)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("want exactly one line, got %d:\n%s", len(lines), out.String())
	}
	if !strings.Contains(lines[0], "level=ERROR") || !strings.Contains(lines[0], `msg="service failed"`) ||
		!strings.Contains(lines[0], "127.0.0.1:1") {
		t.Errorf("line = %s, want a text ERROR service failed naming the address", lines[0])
	}
	if strings.Contains(out.String(), "supersecret") {
		t.Errorf("the output leaks the password:\n%s", out.String())
	}
}

// --- run: the whole service against a real database ---

func TestRun_ServesThenShutsDownInOrder(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: needs Docker")
	}

	pg, stop, err := pgtest.Start(context.Background())
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() {
		if err := stop(context.Background()); err != nil {
			t.Errorf("stop postgres: %v", err)
		}
	})

	cfg := config.Config{
		HTTP: config.HTTP{
			Addr:              "127.0.0.1:0",
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      10 * time.Second,
			IdleTimeout:       60 * time.Second,
			RequestTimeout:    8 * time.Second,
			ShutdownTimeout:   10 * time.Second,
		},
		Log:      config.Log{Format: config.LogFormatJSON},
		Postgres: pg,
	}
	var out syncBuffer
	log := logger.New(&out, cfg.Log)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg, log) }()

	addr, ok := waitForLine(t, &out, "server started")["addr"].(string)
	if !ok {
		t.Fatalf("the server started line has no addr:\n%s", out.String())
	}
	for _, path := range []string{"/health", "/ready"} {
		resp, err := http.Get("http://" + addr + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s = %d %s, want 200", path, resp.StatusCode, body)
		}
	}

	cancel() // the signal
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run() error = %v, want nil after a clean shutdown", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("run() did not return after the signal")
	}

	var order []string
	for _, line := range decodeLines(t, out.String()) {
		msg, _ := line["msg"].(string)
		switch msg {
		case "migrations applied", "server started", "shutting down", "server stopped":
			order = append(order, msg)
		}
	}
	want := []string{"migrations applied", "server started", "shutting down", "server stopped"}
	if fmt.Sprint(order) != fmt.Sprint(want) {
		t.Errorf("lifecycle lines = %v, want %v", order, want)
	}
}

// waitForLine waits up to 15s for a log line with msg and returns it.
func waitForLine(t *testing.T, out *syncBuffer, msg string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		for _, line := range decodeLines(t, out.String()) {
			if line["msg"] == msg {
				return line
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no %q line within 15s:\n%s", msg, out.String())
	return nil
}

func decodeLines(t *testing.T, s string) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(s), "\n") {
		if raw == "" {
			continue
		}
		var line map[string]any
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			t.Fatalf("log line is not JSON: %v\n%s", err, raw)
		}
		lines = append(lines, line)
	}
	return lines
}

// syncBuffer is a bytes.Buffer safe to write from the service's goroutines
// and read from the test's.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
