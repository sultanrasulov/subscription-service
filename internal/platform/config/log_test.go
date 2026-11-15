package config_test

import (
	"log/slog"
	"testing"

	"github.com/sultanrasulov/subscription-service/internal/platform/config"
)

func TestLoad_LogDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(base())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := config.Log{Level: slog.LevelInfo, Format: config.LogFormatJSON}
	if cfg.Log != want {
		t.Errorf("Log = %+v, want %+v", cfg.Log, want)
	}
}

func TestLoad_LogLevels(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]slog.Level{
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg, err := config.Load(with("LOG_LEVEL", name))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.Log.Level != want {
				t.Errorf("Log.Level = %v, want %v", cfg.Log.Level, want)
			}
		})
	}
}

func TestLoad_RejectsUnknownLogLevel(t *testing.T) {
	t.Parallel()

	checkRejected(t, []invalid{
		{"LOG_LEVEL", "verbose", `"verbose"`},
	})
}

func TestLoad_LogFormats(t *testing.T) {
	t.Parallel()

	for value, want := range map[string]config.LogFormat{
		"json": config.LogFormatJSON,
		"text": config.LogFormatText,
	} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()

			cfg, err := config.Load(with("LOG_FORMAT", value))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.Log.Format != want {
				t.Errorf("Log.Format = %q, want %q", cfg.Log.Format, want)
			}
		})
	}
}

func TestLoad_RejectsUnknownLogFormat(t *testing.T) {
	t.Parallel()

	checkRejected(t, []invalid{
		{"LOG_FORMAT", "yaml", `"yaml" must be json or text`},
		{"LOG_FORMAT", "JSON", `"JSON" must be json or text`},
	})
}

// An empty value for a variable that has a default means the default, as in
// most environment-based configuration: compose and shells often pass empty
// variables through. The env library does this for every such variable.
func TestLoad_EmptyValueMeansDefault(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(with("LOG_FORMAT", "", "LOG_LEVEL", ""))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Log.Format != config.LogFormatJSON || cfg.Log.Level != slog.LevelInfo {
		t.Errorf("Log = %+v, want the defaults", cfg.Log)
	}
}
