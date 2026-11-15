// Package httpserver is the service's HTTP server: its middleware, its
// operational routes and its lifecycle. It knows nothing about subscriptions;
// the HTTP adapter registers the API on its mux.
package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/sultanrasulov/subscription-service/internal/platform/logger"
	"github.com/sultanrasulov/subscription-service/internal/platform/problem"
)

// requestIDHeader carries the request ID in both directions.
const requestIDHeader = "X-Request-ID"

// statusClientClosedRequest is nginx's 499: the client went away before the
// response was complete. It is only logged, never sent.
const statusClientClosedRequest = 499

// middleware wraps h, outermost first: request ID, access log, panic
// recovery, deadline. The request ID is outermost, so every log line of the
// request carries it. The access log sits outside the recovery, so it logs the
// 500 the recovery writes. The deadline is innermost, so it bounds only the
// handler's work, not the logging or the recovery.
func middleware(h http.Handler, log *slog.Logger, timeout time.Duration) http.Handler {
	h = deadline(timeout)(h)
	h = recoverPanics(log)(h)
	h = accessLog(log)(h)
	return requestID(h)
}

// requestID gives every request an ID: the incoming X-Request-ID when it is
// safe to reuse, a new UUID v7 otherwise. The ID goes into the response header
// and into the request context, where the logger picks it up.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if !validRequestID(id) {
			id = newRequestID()
		}
		w.Header().Set(requestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(logger.WithRequestID(r.Context(), id)))
	})
}

// validRequestID accepts 1–64 visible ASCII characters. Spaces, control
// characters and anything else could forge log lines or break headers.
func validRequestID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for i := range len(id) {
		if c := id[i]; c < 0x21 || c > 0x7e {
			return false
		}
	}
	return true
}

// newRequestID returns a UUID v7. NewV7 fails only if crypto/rand's reader
// does, which its operating system APIs rule out except on legacy Linux. Any
// other uuid function would read the same source and panic, so the fallback is
// the clock, enough to trace one request.
func newRequestID() string {
	if id, err := uuid.NewV7(); err == nil {
		return id.String()
	}
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

// accessLog writes one "request completed" line per request.
//
// A request whose client went away is logged as status 499 at WARN: nothing
// reaches the client, and it is not a server error. The client going away
// cancels the context (context.Canceled), while an expired request deadline
// gives context.DeadlineExceeded, so the two are told apart in any order.
//
// The line is written in a defer: code after next.ServeHTTP does not run when
// a panic passes through, and the panic recovery re-panics
// http.ErrAbortHandler, so an aborted request is still logged once.
func accessLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
			defer func() {
				status, level := rw.status, slog.LevelInfo
				if errors.Is(r.Context().Err(), context.Canceled) {
					status, level = statusClientClosedRequest, slog.LevelWarn
				}
				log.LogAttrs(r.Context(), level, "request completed",
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Int("status", status),
					slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
				)
			}()
			next.ServeHTTP(rw, r)
		})
	}
}

// recoverPanics turns a panic into a logged error and, if no response has
// started yet, a 500 problem+json. Once the header is sent, a second response
// would corrupt the first, so the panic is only logged.
//
// http.ErrAbortHandler is re-panicked: it is how a handler asks net/http to
// abort the response, and net/http handles it without logging.
func recoverPanics(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(v)
				}
				log.ErrorContext(r.Context(), "panic recovered",
					"panic", v,
					"stack", string(debug.Stack()),
				)
				if !rw.wroteHeader {
					problem.Write(rw, http.StatusInternalServerError, "internal server error")
				}
			}()
			next.ServeHTTP(rw, r)
		})
	}
}

// deadline gives every request context a deadline. The context
// reaches pgx, so a hung query is canceled. Turning an expired deadline into
// a 503 is the HTTP adapter's job, in its error mapping.
func deadline(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// responseWriter records the status and whether the header was sent. Unwrap
// lets http.ResponseController reach Flush and the deadlines underneath.
type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *responseWriter) WriteHeader(status int) {
	if !w.wroteHeader {
		w.status = status
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// FlushError flushes through to the underlying writer. A flush sends the
// header, so it counts as writing it: a later panic must not start a second
// response, and the access log records the status the client got.
// http.ResponseController looks for FlushError before it unwraps.
func (w *responseWriter) FlushError() error {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
