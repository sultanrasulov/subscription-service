package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sultanrasulov/subscription-service/internal/platform/config"
	"github.com/sultanrasulov/subscription-service/internal/platform/logger"
	"github.com/sultanrasulov/subscription-service/internal/platform/problem"
)

// testLogger returns a JSON logger that writes to buf.
func testLogger(buf *bytes.Buffer) *slog.Logger {
	return logger.New(buf, config.Log{Level: slog.LevelDebug, Format: config.LogFormatJSON})
}

// logLines decodes every JSON log line in buf.
func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
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

// linesWithMsg returns the log lines whose msg is msg.
func linesWithMsg(lines []map[string]any, msg string) []map[string]any {
	var found []map[string]any
	for _, line := range lines {
		if line["msg"] == msg {
			found = append(found, line)
		}
	}
	return found
}

// --- requestID ---

func TestRequestID_ReusesValidIncomingID(t *testing.T) {
	t.Parallel()

	for name, id := range map[string]string{
		"short":    "abc-123",
		"64 bytes": strings.Repeat("a", 64),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var seen string
			h := requestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				seen = logger.RequestID(r.Context())
			}))
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			req.Header.Set("X-Request-ID", id)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if got := rec.Header().Get("X-Request-ID"); got != id {
				t.Errorf("response X-Request-ID = %q, want %q", got, id)
			}
			if seen != id {
				t.Errorf("handler saw request ID %q, want %q", seen, id)
			}
		})
	}
}

func TestRequestID_ReplacesInvalidIncomingID(t *testing.T) {
	t.Parallel()

	for name, id := range map[string]*string{
		"absent":    nil,
		"empty":     ptr(""),
		"65 bytes":  ptr(strings.Repeat("a", 65)),
		"newline":   ptr("bad\nid"),
		"space":     ptr("has space"),
		"non-ASCII": ptr("naïve"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var seen string
			h := requestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				seen = logger.RequestID(r.Context())
			}))
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			if id != nil {
				req.Header.Set("X-Request-ID", *id)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			got := rec.Header().Get("X-Request-ID")
			if id != nil && got == *id {
				t.Fatalf("response X-Request-ID = %q, want a new ID", got)
			}
			parsed, err := uuid.Parse(got)
			if err != nil || parsed.Version() != 7 {
				t.Errorf("response X-Request-ID = %q, want a UUID v7", got)
			}
			if seen != got {
				t.Errorf("handler saw request ID %q, want %q", seen, got)
			}
		})
	}
}

func ptr(s string) *string { return &s }

// --- accessLog ---

func TestAccessLog_WritesOneLinePerRequest(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := requestID(accessLog(testLogger(&buf))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/subscriptions?x=1", nil))

	lines := logLines(t, &buf)
	if len(lines) != 1 {
		t.Fatalf("want exactly one log line, got %d:\n%s", len(lines), buf.String())
	}
	line := lines[0]
	if line["msg"] != "request completed" || line["level"] != "INFO" || line["method"] != "POST" ||
		line["path"] != "/api/v1/subscriptions" || line["status"] != 201.0 {
		t.Errorf("line = %v", line)
	}
	if _, ok := line["duration_ms"].(float64); !ok {
		t.Errorf("duration_ms = %v, want a number", line["duration_ms"])
	}
	if line["request_id"] != rec.Header().Get("X-Request-ID") {
		t.Errorf("request_id = %v, want the response's X-Request-ID %q", line["request_id"], rec.Header().Get("X-Request-ID"))
	}
}

func TestAccessLog_DefaultStatusIs200(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := accessLog(testLogger(&buf))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

	if line := logLines(t, &buf)[0]; line["status"] != 200.0 {
		t.Errorf("status = %v, want 200", line["status"])
	}
}

func TestAccessLog_ClientDisconnectIsLoggedAs499(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	// The client goes away while the handler runs; nothing is written.
	h := accessLog(testLogger(&buf))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		cancel()
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil).WithContext(ctx))

	line := logLines(t, &buf)[0]
	if line["status"] != 499.0 || line["level"] != "WARN" {
		t.Errorf("line = %v, want status 499 at WARN", line)
	}
}

// On a real server, net/http cancels the request context when the client
// closes the connection; the access log must see that as 499.
func TestAccessLog_RealClientDisconnectIsLoggedAs499(t *testing.T) {
	t.Parallel()

	var buf syncBuffer
	handlerStarted := make(chan struct{})
	srv := httptest.NewServer(accessLog(logger.New(&buf, config.Log{Format: config.LogFormatJSON}))(
		http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			close(handlerStarted)
			<-r.Context().Done()
		})))
	defer srv.Close()

	ctx, cancel := context.WithCancel(t.Context())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	go func() {
		<-handlerStarted
		cancel()
	}()
	if resp, err := srv.Client().Do(req); err == nil {
		_ = resp.Body.Close()
		t.Fatal("the request completed, want it canceled by the client")
	}

	// The access log line is written after the handler returns.
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(buf.String(), "request completed") {
		if time.Now().After(deadline) {
			t.Fatal("no access log line within 5s")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var line map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &line); err != nil {
		t.Fatalf("log line: %v\n%s", err, buf.String())
	}
	if line["status"] != 499.0 || line["level"] != "WARN" {
		t.Errorf("line = %v, want status 499 at WARN", line)
	}
}

// syncBuffer is a bytes.Buffer safe to write from the server's goroutine and
// read from the test's.
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

// --- recoverPanics ---

func TestRecoverPanics_PanicBecomesProblem500(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := testLogger(&buf)
	h := accessLog(log)(recoverPanics(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != problem.ContentType {
		t.Errorf("Content-Type = %q, want %q", got, problem.ContentType)
	}
	if !strings.Contains(rec.Body.String(), `"detail":"internal server error"`) {
		t.Errorf("body = %s, want the internal server error problem", rec.Body.String())
	}

	lines := logLines(t, &buf)
	panics := linesWithMsg(lines, "panic recovered")
	if len(panics) != 1 {
		t.Fatalf("want one panic line, got:\n%s", buf.String())
	}
	if panics[0]["level"] != "ERROR" || panics[0]["panic"] != "boom" {
		t.Errorf("panic line = %v", panics[0])
	}
	if stack, _ := panics[0]["stack"].(string); !strings.Contains(stack, "goroutine") {
		t.Errorf("panic line has no stack: %v", panics[0])
	}
	if done := linesWithMsg(lines, "request completed"); len(done) != 1 || done[0]["status"] != 500.0 {
		t.Errorf("want one access line with status 500, got:\n%s", buf.String())
	}
}

func TestRecoverPanics_AfterHeadersSentOnlyLogs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := recoverPanics(testLogger(&buf))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial"))
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if rec.Code != http.StatusOK || rec.Body.String() != "partial" {
		t.Errorf("response = %d %q, want 200 %q with no problem appended", rec.Code, rec.Body.String(), "partial")
	}
	if len(linesWithMsg(logLines(t, &buf), "panic recovered")) != 1 {
		t.Errorf("want one panic line, got:\n%s", buf.String())
	}
}

// A flush sends the header. A panic after it must not start a second
// response, and the access log must record the status the client got.
func TestRecoverPanics_FlushThenPanicKeepsResponse(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := testLogger(&buf)
	h := accessLog(log)(recoverPanics(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("Flush: %v", err)
		}
		panic("boom")
	})))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Errorf("response = %d %q, want the flushed 200 with no problem appended", rec.Code, rec.Body.String())
	}
	done := linesWithMsg(logLines(t, &buf), "request completed")
	if len(done) != 1 || done[0]["status"] != 200.0 {
		t.Errorf("want one access line with status 200, got:\n%s", buf.String())
	}
}

func TestRecoverPanics_RepanicsErrAbortHandler(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := testLogger(&buf)
	h := accessLog(log)(recoverPanics(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	})))

	defer func() {
		v := recover()
		if err, ok := v.(error); !ok || !errors.Is(err, http.ErrAbortHandler) {
			t.Errorf("recovered %v, want http.ErrAbortHandler re-panicked", v)
		}
		if strings.Contains(buf.String(), "panic recovered") {
			t.Errorf("ErrAbortHandler was logged as a panic:\n%s", buf.String())
		}
		// The aborted request is still logged once.
		if n := len(linesWithMsg(logLines(t, &buf), "request completed")); n != 1 {
			t.Errorf("want one access log line for the aborted request, got %d:\n%s", n, buf.String())
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
}

// --- deadline ---

func TestDeadline_SetsRequestDeadline(t *testing.T) {
	t.Parallel()

	var got time.Time
	var ok bool
	h := deadline(2 * time.Second)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, ok = r.Context().Deadline()
	}))
	before := time.Now()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

	if !ok {
		t.Fatal("the request context has no deadline")
	}
	if got.Before(before) || got.After(before.Add(2*time.Second+100*time.Millisecond)) {
		t.Errorf("deadline = %v, want about 2s after %v", got, before)
	}
}

// --- the wrapped ResponseWriter ---

// http.ResponseController finds Flush and the deadlines through Unwrap, so
// the middleware must not hide them from handlers.
func TestMiddleware_KeepsResponseControllerWorking(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := testLogger(&buf)
	var flushErr error
	h := accessLog(log)(recoverPanics(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flushErr = http.NewResponseController(w).Flush()
	})))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if flushErr != nil {
		t.Errorf("Flush through the middleware: %v", flushErr)
	}
	if !rec.Flushed {
		t.Error("the recorder was not flushed")
	}
}

// --- the chain ---

// middleware applies the order the plan fixes: request ID, access log, panic
// recovery, deadline. A handler that sees a deadline and then panics shows all
// four, and every line of the request carries the same request ID.
func TestMiddleware_AppliesTheOrder(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	var sawDeadline bool
	h := middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, sawDeadline = r.Context().Deadline()
		panic("boom")
	}), testLogger(&buf), 2*time.Second)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if !sawDeadline {
		t.Error("the handler saw no deadline")
	}
	id := rec.Header().Get("X-Request-ID")
	if id == "" {
		t.Fatal("the response has no X-Request-ID")
	}
	if rec.Code != http.StatusInternalServerError || rec.Header().Get("Content-Type") != problem.ContentType {
		t.Errorf("response = %d %q, want a 500 problem", rec.Code, rec.Header().Get("Content-Type"))
	}

	lines := logLines(t, &buf)
	panics, done := linesWithMsg(lines, "panic recovered"), linesWithMsg(lines, "request completed")
	if len(panics) != 1 || len(done) != 1 {
		t.Fatalf("want one panic line and one access line, got:\n%s", buf.String())
	}
	if panics[0]["request_id"] != id || done[0]["request_id"] != id {
		t.Errorf("request IDs = %v and %v, want both %q", panics[0]["request_id"], done[0]["request_id"], id)
	}
	if done[0]["status"] != 500.0 {
		t.Errorf("access line status = %v, want 500", done[0]["status"])
	}
}
