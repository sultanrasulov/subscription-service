package logger_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"testing/slogtest"

	"github.com/sultanrasulov/subscription-service/internal/platform/config"
	"github.com/sultanrasulov/subscription-service/internal/platform/logger"
)

var formats = []config.LogFormat{config.LogFormatJSON, config.LogFormatText}

func newLogger(buf *bytes.Buffer, format config.LogFormat) *slog.Logger {
	return logger.New(buf, config.Log{Level: slog.LevelInfo, Format: format})
}

// decodeJSON decodes the single JSON line in buf.
func decodeJSON(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("output is not one JSON object: %v\n%s", err, buf.String())
	}
	return line
}

func TestNew_JSONFormat(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	newLogger(&buf, config.LogFormatJSON).InfoContext(t.Context(), "hello", "k", 1)

	line := decodeJSON(t, &buf)
	if line["msg"] != "hello" || line["level"] != "INFO" || line["k"] != 1.0 {
		t.Errorf("line = %v, want msg hello, level INFO, k 1", line)
	}
	if _, ok := line["time"]; !ok {
		t.Errorf("line has no time: %v", line)
	}
}

func TestNew_TextFormat(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	newLogger(&buf, config.LogFormatText).InfoContext(t.Context(), "hello", "k", 1)

	out := buf.String()
	for _, want := range []string{"level=INFO", "msg=hello", "k=1", "time="} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not contain %q:\n%s", want, out)
		}
	}
	if strings.HasPrefix(out, "{") {
		t.Errorf("text output looks like JSON:\n%s", out)
	}
}

func TestNew_AddsRequestIDFromContext(t *testing.T) {
	t.Parallel()

	for _, format := range formats {
		t.Run(string(format), func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			ctx := logger.WithRequestID(t.Context(), "req-1")
			newLogger(&buf, format).InfoContext(ctx, "hello")

			assertTopLevelRequestID(t, &buf, format, "req-1")
		})
	}
}

func TestNew_OmitsRequestIDWhenAbsent(t *testing.T) {
	t.Parallel()

	for _, format := range formats {
		for name, ctx := range map[string]context.Context{
			"no ID":    t.Context(),
			"empty ID": logger.WithRequestID(t.Context(), ""),
		} {
			t.Run(string(format)+"/"+name, func(t *testing.T) {
				t.Parallel()

				var buf bytes.Buffer
				newLogger(&buf, format).InfoContext(ctx, "hello")

				if strings.Contains(buf.String(), "request_id") {
					t.Errorf("output has request_id:\n%s", buf.String())
				}
			})
		}
	}
}

func TestNew_KeepsRequestIDTopLevelAfterWithAndWithGroup(t *testing.T) {
	t.Parallel()

	for _, format := range formats {
		t.Run(string(format), func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			ctx := logger.WithRequestID(t.Context(), "req-1")
			// Two attrs in one With and two nested groups, so a replay that
			// keeps only the last attr or flattens the groups fails.
			newLogger(&buf, format).
				With("component", "x").
				WithGroup("g").
				With("b", 2, "c", 3).
				WithGroup("h").
				InfoContext(ctx, "m", "a", 1)

			assertTopLevelRequestID(t, &buf, format, "req-1")
			if format == config.LogFormatJSON {
				line := decodeJSON(t, &buf)
				g, _ := line["g"].(map[string]any)
				h, _ := g["h"].(map[string]any)
				if line["component"] != "x" || g["b"] != 2.0 || g["c"] != 3.0 || h["a"] != 1.0 {
					t.Errorf("line = %v, want component x, g.b 2, g.c 3 and g.h.a 1", line)
				}
				return
			}
			for _, want := range []string{"component=x", "g.b=2", "g.c=3", "g.h.a=1"} {
				if !strings.Contains(buf.String(), want) {
					t.Errorf("output does not contain %q:\n%s", want, buf.String())
				}
			}
		})
	}
}

func TestNew_ZeroFormatWritesJSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger.New(&buf, config.Log{}).InfoContext(t.Context(), "hello")

	if line := decodeJSON(t, &buf); line["msg"] != "hello" {
		t.Errorf("line = %v, want msg hello", line)
	}
}

func TestNew_RespectsLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	info := logger.New(&buf, config.Log{Level: slog.LevelInfo, Format: config.LogFormatJSON})
	info.DebugContext(t.Context(), "hidden")
	if buf.Len() != 0 {
		t.Errorf("info logger wrote a debug record:\n%s", buf.String())
	}

	debug := logger.New(&buf, config.Log{Level: slog.LevelDebug, Format: config.LogFormatJSON})
	debug.DebugContext(t.Context(), "shown")
	if !strings.Contains(buf.String(), "shown") {
		t.Errorf("debug logger dropped a debug record:\n%s", buf.String())
	}
}

func TestRequestID_RoundTrip(t *testing.T) {
	t.Parallel()

	if got := logger.RequestID(logger.WithRequestID(t.Context(), "x")); got != "x" {
		t.Errorf("RequestID() = %q, want %q", got, "x")
	}
	if got := logger.RequestID(t.Context()); got != "" {
		t.Errorf("RequestID() without an ID = %q, want empty", got)
	}
}

// assertTopLevelRequestID checks that the record carries request_id at the top
// level, never inside a group.
func assertTopLevelRequestID(t *testing.T, buf *bytes.Buffer, format config.LogFormat, want string) {
	t.Helper()

	if format == config.LogFormatJSON {
		if got := decodeJSON(t, buf)["request_id"]; got != want {
			t.Errorf("top-level request_id = %v, want %q\n%s", got, want, buf.String())
		}
		return
	}
	out := buf.String()
	if !strings.Contains(out, " request_id="+want) {
		t.Errorf("output has no top-level request_id=%s:\n%s", want, out)
	}
	if strings.Contains(out, ".request_id=") {
		t.Errorf("request_id is inside a group:\n%s", out)
	}
}

// TestHandler_Conformance runs the standard library's slog.Handler test suite
// against the handler New builds, so WithAttrs, WithGroup and their edge cases
// (empty groups, zero attrs, nested groups) behave as slog requires.
//
// slogtest logs without a context, so this run reaches only the path without
// a request ID. TestHandler_ConformanceWithRequestID covers the replay.
func TestHandler_Conformance(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	slogtest.Run(t,
		func(*testing.T) slog.Handler {
			buf.Reset()
			return newLogger(&buf, config.LogFormatJSON).Handler()
		},
		func(t *testing.T) map[string]any {
			return decodeJSON(t, &buf)
		},
	)
}

// TestHandler_ConformanceWithRequestID runs the slogtest suite again, with a
// request ID in every record's context, so every case goes through the replay
// of WithAttrs and WithGroup. The ID must be at the top level; it is removed
// before slogtest compares the rest.
func TestHandler_ConformanceWithRequestID(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	slogtest.Run(t,
		func(*testing.T) slog.Handler {
			buf.Reset()
			return withRequestID{newLogger(&buf, config.LogFormatJSON).Handler()}
		},
		func(t *testing.T) map[string]any {
			line := decodeJSON(t, &buf)
			if line["request_id"] != "req-1" {
				t.Errorf("top-level request_id = %v, want %q", line["request_id"], "req-1")
			}
			delete(line, "request_id")
			return line
		},
	)
}

// withRequestID passes every record a context carrying a request ID: slogtest
// logs without a context, so it would never reach the path that adds the ID.
type withRequestID struct{ slog.Handler }

func (h withRequestID) Handle(ctx context.Context, r slog.Record) error {
	return h.Handler.Handle(logger.WithRequestID(ctx, "req-1"), r)
}

func (h withRequestID) WithAttrs(attrs []slog.Attr) slog.Handler {
	return withRequestID{h.Handler.WithAttrs(attrs)}
}

func (h withRequestID) WithGroup(name string) slog.Handler {
	return withRequestID{h.Handler.WithGroup(name)}
}
