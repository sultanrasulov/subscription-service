package config

import "log/slog"

// redacted replaces a secret wherever it would otherwise be printed.
const redacted = "[REDACTED]"

// Secret is a configuration value that must never be printed or logged
// (CLAUDE.md, "Never log secrets").
//
// fmt (with %v, %+v, %#v, %s and %q), slog and encoding/json all show
// "[REDACTED]" instead of the value, even when a whole configuration struct
// is printed. Reveal returns the value for the one place that needs it, such
// as a connection string.
//
// Secret deliberately does not implement fmt.Formatter. With a wrong verb
// such as %d, fmt prints the raw value inside its error text, and a Formatter
// would not help: it would only stop go vet from rejecting such verbs for this
// type, and %p bypasses it anyway. vet runs in go test and in the linter, so
// a wrong verb fails the build instead of leaking silently.
type Secret string

// Reveal returns the secret value. Call it only where the value is used,
// never to print it.
func (s Secret) Reveal() string { return string(s) }

// String implements fmt.Stringer for the %v, %+v, %s and %q verbs.
func (Secret) String() string { return redacted }

// GoString implements fmt.GoStringer for the %#v verb.
func (Secret) GoString() string { return redacted }

// LogValue implements slog.LogValuer.
func (Secret) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalText implements encoding.TextMarshaler, which encoding/json uses
// and slog's handlers use for values that are not LogValuers.
func (Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }
