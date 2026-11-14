package httpserver

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/sultanrasulov/subscription-service/internal/platform/problem"
)

const testSpec = `{"swagger":"2.0"}`

// testOptions returns Options with a logger writing to buf, a healthy
// readiness check and a generated spec. Tests change what they need.
func testOptions(buf *bytes.Buffer) Options {
	return Options{
		Logger:         testLogger(buf),
		Ready:          func(context.Context) error { return nil },
		Swagger:        fstest.MapFS{"swagger.json": {Data: []byte(testSpec)}},
		RequestTimeout: time.Second,
	}
}

func serve(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func assertProblem(t *testing.T, rec *httptest.ResponseRecorder, status int, detail string) {
	t.Helper()
	if rec.Code != status {
		t.Errorf("status = %d, want %d", rec.Code, status)
	}
	if got := rec.Header().Get("Content-Type"); got != problem.ContentType {
		t.Errorf("Content-Type = %q, want %q", got, problem.ContentType)
	}
	if !strings.Contains(rec.Body.String(), `"detail":"`+detail+`"`) {
		t.Errorf("body = %s, want detail %q", rec.Body.String(), detail)
	}
}

// --- /health and /ready ---

func TestHealth_AlwaysOK(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	opts := testOptions(&buf)
	opts.Ready = func(context.Context) error { return errors.New("db down") }
	rec := serve(NewHandler(opts), http.MethodGet, "/health")

	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" ||
		strings.TrimSpace(rec.Body.String()) != `{"status":"ok"}` {
		t.Errorf("response = %d %q %s, want 200 application/json {\"status\":\"ok\"}",
			rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
}

func TestReady_OK(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	rec := serve(NewHandler(testOptions(&buf)), http.MethodGet, "/ready")

	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"status":"ok"}` {
		t.Errorf("response = %d %s, want 200 {\"status\":\"ok\"}", rec.Code, rec.Body.String())
	}
}

func TestReady_FailingCheck(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	opts := testOptions(&buf)
	opts.Ready = func(context.Context) error { return errors.New("connection refused") }
	rec := serve(NewHandler(opts), http.MethodGet, "/ready")

	assertProblem(t, rec, http.StatusServiceUnavailable, "database is not ready")
	warns := linesWithMsg(logLines(t, &buf), "readiness check failed")
	if len(warns) != 1 || warns[0]["level"] != "WARN" || warns[0]["error"] != "connection refused" {
		t.Errorf("want one WARN readiness line with the error, got:\n%s", buf.String())
	}
}

func TestReady_SlowCheckTimesOutAfterOneSecond(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	opts := testOptions(&buf)
	opts.RequestTimeout = 5 * time.Second
	var deadline time.Time
	opts.Ready = func(ctx context.Context) error {
		deadline, _ = ctx.Deadline()
		<-ctx.Done()
		return ctx.Err()
	}
	start := time.Now()
	rec := serve(NewHandler(opts), http.MethodGet, "/ready")

	assertProblem(t, rec, http.StatusServiceUnavailable, "database is not ready")
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("/ready took %v, want about 1s", elapsed)
	}
	if deadline.IsZero() || deadline.After(start.Add(time.Second+100*time.Millisecond)) {
		t.Errorf("readiness deadline = %v, want within 1s of %v", deadline, start)
	}
}

// --- router errors ---

func TestRouterErrors_UnknownPathIsProblem404(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	assertProblem(t, serve(NewHandler(testOptions(&buf)), http.MethodGet, "/nope"), http.StatusNotFound, "not found")
}

func TestRouterErrors_WrongMethodIsProblem405WithAllow(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	rec := serve(NewHandler(testOptions(&buf)), http.MethodPost, "/health")

	assertProblem(t, rec, http.StatusMethodNotAllowed, "method not allowed")
	if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
		t.Errorf("Allow = %q, want %q", got, "GET, HEAD")
	}
}

func TestRouterErrors_LeaveOwnHandlersAlone(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	opts := testOptions(&buf)
	opts.Register = func(mux *http.ServeMux) {
		mux.HandleFunc("GET /api/v1/x", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "mine", http.StatusNotFound)
		})
	}
	rec := serve(NewHandler(opts), http.MethodGet, "/api/v1/x")

	if rec.Code != http.StatusNotFound || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") ||
		strings.TrimSpace(rec.Body.String()) != "mine" {
		t.Errorf("response = %d %q %q, want the handler's own text/plain 404", rec.Code,
			rec.Header().Get("Content-Type"), rec.Body.String())
	}
}

func TestRouterErrors_PassRedirectsThrough(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	// ServeMux redirects a path that is not in canonical form. It reports the
	// pattern the redirect leads to, so the rewrite never sees the redirect.
	rec := serve(NewHandler(testOptions(&buf)), http.MethodGet, "/x/../health")

	// The redirect status is the standard library's choice; what matters is
	// that the redirect reaches the client unchanged.
	if rec.Code < 300 || rec.Code > 399 || rec.Header().Get("Location") != "/health" {
		t.Errorf("response = %d Location %q, want a redirect to /health", rec.Code, rec.Header().Get("Location"))
	}
}

// ServeMux reports an empty pattern for a non-canonical path that matches
// nothing, and answers it with a redirect to the canonical path. The redirect
// passes through; the canonical path then gets the right problem.
func TestRouterErrors_UnmatchedNonCanonicalPathRedirectsThen404(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewHandler(testOptions(&buf))

	rec := serve(h, http.MethodGet, "/nope/../nope2")
	if rec.Code < 300 || rec.Code > 399 || rec.Header().Get("Location") != "/nope2" {
		t.Errorf("response = %d Location %q, want a redirect to /nope2", rec.Code, rec.Header().Get("Location"))
	}
	assertProblem(t, serve(h, http.MethodGet, "/nope2"), http.StatusNotFound, "not found")
}

// A non-canonical path to a route that exists for another method must end in
// 405, not 404: answering 404 at once would skip what the canonical path says.
func TestRouterErrors_NonCanonicalPathToOtherMethodRedirectsThen405(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewHandler(testOptions(&buf))

	rec := serve(h, http.MethodPost, "/x/../health")
	if rec.Code < 300 || rec.Code > 399 || rec.Header().Get("Location") != "/health" {
		t.Errorf("response = %d Location %q, want a redirect to /health", rec.Code, rec.Header().Get("Location"))
	}
	final := serve(h, http.MethodPost, "/health")
	assertProblem(t, final, http.StatusMethodNotAllowed, "method not allowed")
	if got := final.Header().Get("Allow"); got != "GET, HEAD" {
		t.Errorf("Allow = %q, want %q", got, "GET, HEAD")
	}
}

// Routes added through Register read path values such as {id}. Only
// ServeMux.ServeHTTP sets them, so the rewrite must not call the handler that
// mux.Handler returns.
func TestHandler_PathValuesReachRegisteredRoutes(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	opts := testOptions(&buf)
	opts.Register = func(mux *http.ServeMux) {
		mux.HandleFunc("GET /api/v1/items/{id}", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(r.PathValue("id")))
		})
	}
	if rec := serve(NewHandler(opts), http.MethodGet, "/api/v1/items/42"); rec.Body.String() != "42" {
		t.Errorf("body = %q, want the path value %q", rec.Body.String(), "42")
	}
}

// --- Swagger ---

func TestSwagger_ServesEmbeddedSpec(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	rec := serve(NewHandler(testOptions(&buf)), http.MethodGet, "/swagger/doc.json")

	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" || rec.Body.String() != testSpec {
		t.Errorf("response = %d %q %s, want 200 application/json with the spec",
			rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
}

func TestSwagger_MissingSpec(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	opts := testOptions(&buf)
	opts.Swagger = fstest.MapFS{}
	h := NewHandler(opts)

	warns := linesWithMsg(logLines(t, &buf), "swagger spec not embedded, run make swagger")
	if len(warns) != 1 || warns[0]["level"] != "WARN" {
		t.Errorf("want one WARN at construction, got:\n%s", buf.String())
	}
	assertProblem(t, serve(h, http.MethodGet, "/swagger/doc.json"), http.StatusNotFound,
		"swagger specification was not generated")
}

func TestSwagger_UI(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewHandler(testOptions(&buf))

	if rec := serve(h, http.MethodGet, "/swagger"); rec.Header().Get("Location") != "/swagger/" {
		t.Errorf("/swagger = %d Location %q, want a redirect to /swagger/", rec.Code, rec.Header().Get("Location"))
	}
	if rec := serve(h, http.MethodGet, "/swagger/"); rec.Header().Get("Location") != "/swagger/index.html" {
		t.Errorf("/swagger/ = %d Location %q, want a redirect to /swagger/index.html", rec.Code, rec.Header().Get("Location"))
	}
	rec := serve(h, http.MethodGet, "/swagger/index.html")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Errorf("/swagger/index.html = %d %q, want 200 text/html", rec.Code, rec.Header().Get("Content-Type"))
	}
	// The page must load our spec. The UI library also ships a static
	// swagger-initializer.js that points at the petstore demo; the page it
	// renders configures the URL inline instead and must not use that file.
	page := rec.Body.String()
	if !strings.Contains(page, `\/swagger\/doc.json`) || strings.Contains(page, "swagger-initializer.js") {
		t.Errorf("index.html does not load /swagger/doc.json on its own:\n%s", page)
	}
	// Assets are served from the binary, not a CDN.
	if css := serve(h, http.MethodGet, "/swagger/swagger-ui.css"); css.Code != http.StatusOK {
		t.Errorf("/swagger/swagger-ui.css = %d, want 200", css.Code)
	}
}

// --- middleware ---

func TestHandler_AppliesMiddleware(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	rec := serve(NewHandler(testOptions(&buf)), http.MethodGet, "/health")

	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("the response has no X-Request-ID")
	}
	if n := len(linesWithMsg(logLines(t, &buf), "request completed")); n != 1 {
		t.Errorf("want one access log line, got %d:\n%s", n, buf.String())
	}
}
