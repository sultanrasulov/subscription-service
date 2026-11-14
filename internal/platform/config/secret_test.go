package config_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/sultanrasulov/subscription-service/internal/platform/config"
)

func TestSecret_RevealReturnsTheValue(t *testing.T) {
	t.Parallel()

	if got := config.Secret("s3cr3t").Reveal(); got != "s3cr3t" {
		t.Errorf("Reveal() = %q, want %q", got, "s3cr3t")
	}
}

func TestSecret_FmtNeverShowsTheValue(t *testing.T) {
	t.Parallel()

	s := config.Secret("s3cr3t")
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		out := fmt.Sprintf(verb, s)
		if strings.Contains(out, "s3cr3t") {
			t.Errorf("fmt %s leaks the value: %s", verb, out)
		}
		if !strings.Contains(out, "[REDACTED]") {
			t.Errorf("fmt %s = %s, want it to contain [REDACTED]", verb, out)
		}
	}
}

func TestSecret_SlogNeverShowsTheValue(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ctx := t.Context()
	slog.New(slog.NewJSONHandler(&buf, nil)).InfoContext(ctx, "m", "password", config.Secret("s3cr3t"))
	slog.New(slog.NewTextHandler(&buf, nil)).InfoContext(ctx, "m", "password", config.Secret("s3cr3t"))
	// A whole struct logged as one value goes through encoding/json or fmt.
	slog.New(slog.NewJSONHandler(&buf, nil)).InfoContext(ctx, "m", "pg", config.Postgres{Password: "s3cr3t"})
	slog.New(slog.NewTextHandler(&buf, nil)).InfoContext(ctx, "m", "pg", config.Postgres{Password: "s3cr3t"})

	if strings.Contains(buf.String(), "s3cr3t") {
		t.Errorf("slog leaks the value:\n%s", buf.String())
	}
}

func TestSecret_JSONNeverShowsTheValue(t *testing.T) {
	t.Parallel()

	out, err := json.Marshal(config.Postgres{Password: "s3cr3t"})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if strings.Contains(string(out), "s3cr3t") {
		t.Errorf("json leaks the value: %s", out)
	}
}
