package httpserver

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	httpSwagger "github.com/swaggo/http-swagger/v2"

	"github.com/sultanrasulov/subscription-service/internal/platform/problem"
)

// readyTimeout bounds the readiness check's database ping (design §4.5).
const readyTimeout = time.Second

// Options configures the service's HTTP handler.
type Options struct {
	// Logger receives the access log, panics and operational warnings.
	Logger *slog.Logger

	// Ready reports whether the service can handle requests, for example
	// (*pgxpool.Pool).Ping. /ready calls it with a one-second deadline.
	Ready func(context.Context) error

	// Swagger holds the generated swagger.json at its root. It may lack the
	// file, after `go run` without `make swagger`; the service still runs.
	Swagger fs.FS

	// RequestTimeout is the deadline of every request's context.
	RequestTimeout time.Duration

	// Register adds the API routes to the service's one mux. Phase 3 sets it.
	Register func(mux *http.ServeMux)
}

// NewHandler returns the service's HTTP handler: one mux with the operational
// routes and whatever Register adds, the router's own 404 and 405 rewritten as
// problem+json, all behind the middleware chain.
func NewHandler(o Options) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health)
	mux.HandleFunc("GET /ready", ready(o.Logger, o.Ready))
	mux.HandleFunc("GET /swagger/doc.json", swaggerSpec(o.Logger, o.Swagger))
	// More specific than "GET /swagger/", so ServeMux sends doc.json to the
	// handler above: the UI library would serve it from generated Go code,
	// which this project does not have (design §9).
	mux.Handle("GET /swagger/", httpSwagger.Handler(httpSwagger.URL("/swagger/doc.json")))
	if o.Register != nil {
		o.Register(mux)
	}
	return middleware(routerErrors(mux), o.Logger, o.RequestTimeout)
}

// health is liveness: the process is up and serving. It never touches the
// database, so a database outage does not get healthy instances restarted
// (design §4.5).
func health(w http.ResponseWriter, _ *http.Request) {
	writeOK(w)
}

// ready is readiness: the service can handle requests, which needs the
// database. A failure is a 503 and a warning.
func ready(log *slog.Logger, check func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
		defer cancel()
		if err := check(ctx); err != nil {
			log.WarnContext(r.Context(), "readiness check failed", "error", err)
			problem.Write(w, http.StatusServiceUnavailable, "database is not ready")
			return
		}
		writeOK(w)
	}
}

func writeOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	// Nothing can be done about a failed write to the client here.
	_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
}

// swaggerSpec serves swagger.json from fsys, read once here. A missing or
// unreadable spec is a warning at startup and a 404 for the route, never a
// failure: documentation must not stop the service (design §9).
func swaggerSpec(log *slog.Logger, fsys fs.FS) http.HandlerFunc {
	spec, err := fs.ReadFile(fsys, "swagger.json")
	if err != nil {
		// Construction time, not a request path, so there is no request context.
		log.WarnContext(context.Background(), "swagger spec not embedded, run make swagger", "error", err)
		return func(w http.ResponseWriter, _ *http.Request) {
			problem.Write(w, http.StatusNotFound, "swagger specification was not generated")
		}
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Nothing can be done about a failed write to the client here.
		_, _ = w.Write(spec)
	}
}

// routerErrors rewrites the router's own 404 and 405 responses, which
// ServeMux writes as plain text, into problem+json (design §4.4).
//
// A request that matched a route goes straight to the mux, and so does one
// the mux redirects to a canonical path that matches: for those ServeMux
// reports a pattern. An empty pattern means no route matched, and then the
// router's answer is captured first:
//   - a redirect, for a non-canonical path such as /a/../b, passes through as
//     is, so the canonical path decides between 404 and 405 (verified: POST
//     /x/../health ends in 405, not 404);
//   - a 405 becomes a problem that keeps the router's Allow;
//   - anything else is a 404 problem.
func routerErrors(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// mux.ServeHTTP, not the handler mux.Handler returns: only ServeHTTP
		// sets the request's path values, which the API's {id} routes read
		// (verified).
		if _, pattern := mux.Handler(r); pattern != "" {
			mux.ServeHTTP(w, r)
			return
		}

		captured := &captureWriter{header: http.Header{}}
		mux.ServeHTTP(captured, r)

		switch {
		case captured.status >= 300 && captured.status < 400:
			w.Header().Set("Location", captured.header.Get("Location"))
			w.WriteHeader(captured.status)
		case captured.status == http.StatusMethodNotAllowed:
			w.Header().Set("Allow", captured.header.Get("Allow"))
			problem.Write(w, http.StatusMethodNotAllowed, "method not allowed")
		default:
			problem.Write(w, http.StatusNotFound, "not found")
		}
	})
}

// captureWriter records the router's answer to an unmatched request: the
// status and the headers. The router's plain-text or HTML body is dropped.
type captureWriter struct {
	header http.Header
	status int
}

func (c *captureWriter) Header() http.Header { return c.header }

func (c *captureWriter) WriteHeader(status int) { c.status = status }

func (c *captureWriter) Write(b []byte) (int, error) { return len(b), nil }
