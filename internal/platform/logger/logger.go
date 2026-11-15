// Package logger builds the service's structured logger.
//
// Records are JSON or text, as configured. A request ID stored in the context
// with WithRequestID is added to every record logged with that context, as a
// top-level request_id field in both formats.
//
// New writes wherever w points; main passes os.Stdout, because the service
// emits one event stream and the environment routes it (twelve-factor XI).
package logger

import (
	"context"
	"io"
	"log/slog"

	"github.com/sultanrasulov/subscription-service/internal/platform/config"
)

// requestIDKey is the record field that carries the request ID.
const requestIDKey = "request_id"

// New returns a logger that writes to w at cfg.Level, in cfg.Format.
// Any format other than text is written as JSON, the default.
func New(w io.Writer, cfg config.Log) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.Level}

	var base slog.Handler
	if cfg.Format == config.LogFormatText {
		base = slog.NewTextHandler(w, opts)
	} else {
		base = slog.NewJSONHandler(w, opts)
	}
	return slog.New(&requestIDHandler{base: base, next: base})
}

type contextKey struct{}

// WithRequestID returns a copy of ctx that carries the request ID.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// RequestID returns the request ID carried by ctx, or "" if there is none.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(contextKey{}).(string)
	return id
}

// requestIDHandler adds the context's request ID to each record as a
// top-level field.
//
// Adding the field to the record would put it inside any group opened with
// WithGroup. So the handler records its WithAttrs and WithGroup calls and,
// when a group is open, adds the field to the base handler first and then
// replays them. Without a group, it adds the field to the record directly.
//
// The replay costs allocations per record; it runs only for loggers with a
// group, which the service does not use on hot paths.
type requestIDHandler struct {
	base     slog.Handler // the JSON or text handler, with no attrs or groups
	ops      []op         // WithAttrs and WithGroup calls, in order
	next     slog.Handler // base with ops applied
	hasGroup bool
}

// op is one WithAttrs call (attrs) or one WithGroup call (group).
type op struct {
	attrs []slog.Attr
	group string
}

func (h *requestIDHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *requestIDHandler) Handle(ctx context.Context, r slog.Record) error {
	id := RequestID(ctx)
	if id == "" {
		return h.next.Handle(ctx, r)
	}
	if !h.hasGroup {
		r.AddAttrs(slog.String(requestIDKey, id))
		return h.next.Handle(ctx, r)
	}

	handler := h.base.WithAttrs([]slog.Attr{slog.String(requestIDKey, id)})
	for _, o := range h.ops {
		if o.group != "" {
			handler = handler.WithGroup(o.group)
		} else {
			handler = handler.WithAttrs(o.attrs)
		}
	}
	return handler.Handle(ctx, r)
}

func (h *requestIDHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return h.with(op{attrs: attrs}, h.next.WithAttrs(attrs), h.hasGroup)
}

func (h *requestIDHandler) WithGroup(name string) slog.Handler {
	// slog ignores an empty group name; so does the replay.
	if name == "" {
		return h
	}
	return h.with(op{group: name}, h.next.WithGroup(name), true)
}

func (h *requestIDHandler) with(o op, next slog.Handler, hasGroup bool) *requestIDHandler {
	ops := make([]op, len(h.ops), len(h.ops)+1)
	copy(ops, h.ops)
	return &requestIDHandler{
		base:     h.base,
		ops:      append(ops, o),
		next:     next,
		hasGroup: hasGroup,
	}
}
