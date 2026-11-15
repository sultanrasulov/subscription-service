package config_test

import (
	"testing"
	"time"

	"github.com/sultanrasulov/subscription-service/internal/platform/config"
)

func TestLoad_HTTPDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(base())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := config.HTTP{
		Addr:              ":8080",
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		RequestTimeout:    8 * time.Second,
		ShutdownTimeout:   10 * time.Second,
	}
	if cfg.HTTP != want {
		t.Errorf("HTTP = %+v, want %+v", cfg.HTTP, want)
	}
}

func TestLoad_HTTPValues(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(with(
		"HTTP_ADDR", "127.0.0.1:9090",
		"HTTP_REQUEST_TIMEOUT", "3s",
		"HTTP_SHUTDOWN_TIMEOUT", "20s",
	))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTP.Addr != "127.0.0.1:9090" {
		t.Errorf("Addr = %q, want %q", cfg.HTTP.Addr, "127.0.0.1:9090")
	}
	if cfg.HTTP.RequestTimeout != 3*time.Second {
		t.Errorf("RequestTimeout = %v, want 3s", cfg.HTTP.RequestTimeout)
	}
	if cfg.HTTP.ShutdownTimeout != 20*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 20s", cfg.HTTP.ShutdownTimeout)
	}
}

func TestLoad_RejectsInvalidHTTP(t *testing.T) {
	t.Parallel()

	checkRejected(t, []invalid{
		{"HTTP_ADDR", "8080", `"8080"`},
		{"HTTP_ADDR", ":0", `":0"`},
		{"HTTP_ADDR", ":70000", `":70000"`},
		{"HTTP_ADDR", ":http", `":http"`},
		{"HTTP_READ_HEADER_TIMEOUT", "abc", `"abc"`},
		{"HTTP_READ_HEADER_TIMEOUT", "-1s", "-1s must be positive"},
		{"HTTP_READ_TIMEOUT", "0s", "0s must be positive"},
		{"HTTP_WRITE_TIMEOUT", "0s", "0s must be positive"},
		{"HTTP_IDLE_TIMEOUT", "0s", "0s must be positive"},
		{"HTTP_REQUEST_TIMEOUT", "0s", "0s must be positive"},
		{"HTTP_SHUTDOWN_TIMEOUT", "0s", "0s must be positive"},
		// Not shorter than the 10s write timeout.
		{"HTTP_REQUEST_TIMEOUT", "10s", "10s must be shorter than HTTP_WRITE_TIMEOUT (10s)"},
		// Shorter than the 8s request timeout.
		{"HTTP_SHUTDOWN_TIMEOUT", "5s", "5s must not be shorter than HTTP_REQUEST_TIMEOUT (8s)"},
	})
}
