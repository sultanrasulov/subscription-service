// Package config loads the service configuration from environment variables
// (design §7). It never reads files: docker compose and `make run` put the
// values into the process environment.
//
// Each section of the configuration lives in its own file, together with the
// rules that validate it: http.go, log.go and postgres.go.
package config

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/caarlos0/env/v11"
)

// Config is the whole service configuration.
type Config struct {
	HTTP     HTTP
	Log      Log
	Postgres Postgres
}

// Load reads the configuration from environ, a map of variable names to values.
//
// Every missing or malformed variable is reported at once. Only when all of
// them parse does a second step check their meaning, again reporting every
// problem. Each message starts with the variable's name, and no message
// contains a secret. A nil environ is an empty environment.
func Load(environ map[string]string) (Config, error) {
	// The env library reads the process environment when given a nil map,
	// which would make Load depend on whatever the caller's process exports.
	if environ == nil {
		environ = map[string]string{}
	}
	cfg, err := env.ParseAsWithOptions[Config](env.Options{Environment: environ})
	if err != nil {
		return Config{}, renameParseErrors(err)
	}
	if err := errors.Join(append(cfg.HTTP.validate(), cfg.Postgres.validate()...)...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// problem formats one validation error, starting with the variable's name.
//
// format and args reach fmt.Sprintf unchanged, so go vet recognizes problem
// as a printf wrapper and checks the verbs at every call.
func problem(variable, format string, args ...any) error {
	return errors.New(variable + ": " + fmt.Sprintf(format, args...))
}

func validPort(n int) bool { return n >= 1 && n <= 65535 }

// renameParseErrors rewrites the env library's errors so that each one starts
// with the variable's name. Its parse errors name the Go struct field instead
// (verified), which means nothing to whoever sets the environment.
func renameParseErrors(err error) error {
	var agg env.AggregateError
	if !errors.As(err, &agg) {
		return err
	}

	keys := envKeys(reflect.TypeFor[Config]())
	errs := make([]error, 0, len(agg.Errors))
	for _, e := range agg.Errors {
		var (
			parseErr env.ParseError
			unsetErr env.VarIsNotSetError
			emptyErr env.EmptyVarError
		)
		switch {
		case errors.As(e, &parseErr):
			errs = append(errs, fmt.Errorf("%s: invalid value: %w", keys[parseErr.Name], parseErr.Err))
		case errors.As(e, &unsetErr):
			errs = append(errs, fmt.Errorf("%s: is required", unsetErr.Key))
		case errors.As(e, &emptyErr):
			errs = append(errs, fmt.Errorf("%s: must not be empty", emptyErr.Key))
		default:
			errs = append(errs, e)
		}
	}
	return errors.Join(errs...)
}

// envKeys maps each field name in t, at any depth, to its variable name.
// Field names are unique across Config, so one flat map is enough.
func envKeys(t reflect.Type) map[string]string {
	keys := make(map[string]string)
	for i := range t.NumField() {
		f := t.Field(i)
		tag, ok := f.Tag.Lookup("env")
		if !ok && f.Type.Kind() == reflect.Struct {
			for name, key := range envKeys(f.Type) {
				keys[name] = key
			}
			continue
		}
		keys[f.Name], _, _ = strings.Cut(tag, ",")
	}
	return keys
}
