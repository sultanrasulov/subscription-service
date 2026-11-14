package config

import (
	"fmt"
	"log/slog"
)

// Log configures logging (design §8).
type Log struct {
	// Level accepts the slog level names: debug, info, warn and error.
	// The env library parses it through slog.Level's UnmarshalText, which
	// rejects anything else, so it needs no separate validation.
	Level slog.Level `env:"LOG_LEVEL" envDefault:"info"`

	// Format is json for machines (containers, log collectors) and text for
	// reading logs locally. The default is json, so docker compose needs no
	// setting; a developer sets LOG_FORMAT=text in .env.
	Format LogFormat `env:"LOG_FORMAT" envDefault:"json"`
}

// LogFormat is the log output format: exactly json or text.
type LogFormat string

// The log formats.
const (
	LogFormatJSON LogFormat = "json"
	LogFormatText LogFormat = "text"
)

// UnmarshalText implements encoding.TextUnmarshaler. The env library calls it
// while parsing, so an unknown format is a parse error like any other.
// Matching is exact: "JSON" is a mistake to report, not a synonym.
func (f *LogFormat) UnmarshalText(text []byte) error {
	switch v := LogFormat(text); v {
	case LogFormatJSON, LogFormatText:
		*f = v
		return nil
	default:
		return fmt.Errorf("%q must be %s or %s", text, LogFormatJSON, LogFormatText)
	}
}
