package httpserver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sultanrasulov/subscription-service/internal/platform/config"
)

func TestNewServer_SetsTimeoutsAndErrorLog(t *testing.T) {
	t.Parallel()

	cfg := config.HTTP{
		ReadHeaderTimeout: 1 * time.Second,
		ReadTimeout:       2 * time.Second,
		WriteTimeout:      3 * time.Second,
		IdleTimeout:       4 * time.Second,
	}
	var buf bytes.Buffer
	srv := NewServer(cfg, http.NotFoundHandler(), testLogger(&buf))

	if srv.ReadHeaderTimeout != cfg.ReadHeaderTimeout || srv.ReadTimeout != cfg.ReadTimeout ||
		srv.WriteTimeout != cfg.WriteTimeout || srv.IdleTimeout != cfg.IdleTimeout {
		t.Errorf("timeouts = %v %v %v %v, want %v %v %v %v",
			srv.ReadHeaderTimeout, srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout,
			cfg.ReadHeaderTimeout, cfg.ReadTimeout, cfg.WriteTimeout, cfg.IdleTimeout)
	}

	// net/http's own messages, such as TLS handshake errors, go through slog.
	if srv.ErrorLog == nil {
		t.Fatal("ErrorLog is nil")
	}
	srv.ErrorLog.Print("http: a server message")
	lines := linesWithMsg(logLines(t, &buf), "http: a server message")
	if len(lines) != 1 || lines[0]["level"] != "WARN" {
		t.Errorf("want the server message as one WARN line, got:\n%s", buf.String())
	}
}

// startServe runs Serve on a random local port and returns the address, the
// cancel that starts the shutdown, and a channel with Serve's result.
func startServe(t *testing.T, h http.Handler, shutdownTimeout time.Duration) (string, context.CancelFunc, <-chan error) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var buf bytes.Buffer
	srv := NewServer(config.HTTP{ReadHeaderTimeout: time.Second}, h, testLogger(&buf))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, srv, ln, shutdownTimeout) }()
	return "http://" + ln.Addr().String(), cancel, done
}

func TestServe_GracefulShutdownWaitsForInFlightRequests(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		time.Sleep(300 * time.Millisecond)
		// The shutdown must not cancel the request: its work, such as a
		// database query using this context, has to finish.
		if err := r.Context().Err(); err != nil {
			_, _ = w.Write([]byte("context: " + err.Error()))
			return
		}
		_, _ = w.Write([]byte("done"))
	})
	url, cancel, done := startServe(t, h, 2*time.Second)

	type result struct {
		status int
		body   string
		err    error
	}
	got := make(chan result, 1)
	go func() {
		resp, err := http.Get(url)
		if err != nil {
			got <- result{err: err}
			return
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		got <- result{resp.StatusCode, string(body), err}
	}()

	<-started
	cancel() // SIGTERM arrives while the request is in flight.

	if r := <-got; r.err != nil || r.status != http.StatusOK || r.body != "done" {
		t.Errorf("in-flight request = %d %q %v, want 200 %q", r.status, r.body, r.err, "done")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve() error = %v, want nil after a graceful shutdown", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve() did not return after the shutdown")
	}

	// The listener is closed: a new connection is refused.
	if conn, err := net.DialTimeout("tcp", strings.TrimPrefix(url, "http://"), time.Second); err == nil {
		_ = conn.Close()
		t.Error("a new connection was accepted after the shutdown")
	}
}

func TestServe_ReturnsErrorWhenShutdownTimesOut(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		<-release // a request that outlives the shutdown timeout
	})
	url, cancel, done := startServe(t, h, 100*time.Millisecond)

	client := make(chan error, 1)
	go func() {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
		}
		client <- err
	}()
	<-started
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Serve() error = %v, want one wrapping context.DeadlineExceeded", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve() did not return within 1s of a 100ms shutdown timeout")
	}

	// After the overrun the server is closed, so the stuck request's
	// connection is cut instead of keeping the process alive.
	select {
	case err := <-client:
		if err == nil {
			t.Error("the stuck request succeeded, want its connection closed")
		}
	case <-time.After(time.Second):
		t.Error("the stuck request's connection is still open after Serve returned")
	}
}

func TestServe_ReturnsServeError(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_ = ln.Close() // Serve cannot accept on a closed listener.

	var buf bytes.Buffer
	srv := NewServer(config.HTTP{ReadHeaderTimeout: time.Second}, http.NotFoundHandler(), testLogger(&buf))
	done := make(chan error, 1)
	go func() { done <- Serve(t.Context(), srv, ln, time.Second) }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("Serve() error = nil, want the accept error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve() did not return on a closed listener")
	}
}
