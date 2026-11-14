package config

import (
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Postgres configures the database connection (design §7).
//
// POSTGRES_USER, POSTGRES_PASSWORD and POSTGRES_DB are the names the Postgres
// image reads itself, so one env file configures both the service and the
// database container. The image ignores the other POSTGRES_* variables.
type Postgres struct {
	Host     string `env:"POSTGRES_HOST,required,notEmpty"`
	Port     int    `env:"POSTGRES_PORT"                    envDefault:"5432"`
	User     string `env:"POSTGRES_USER,required,notEmpty"`
	Password Secret `env:"POSTGRES_PASSWORD,required,notEmpty"`
	Database string `env:"POSTGRES_DB,required,notEmpty"`
	SSLMode  string `env:"POSTGRES_SSLMODE"                 envDefault:"disable"`
	MaxConns int32  `env:"POSTGRES_MAX_CONNS"               envDefault:"10"`

	// ConnectTimeout bounds opening every new connection (dial, TLS and
	// authentication) and the ping at startup. Without it pgx waits for the
	// operating system's TCP timeout, so an unreachable database would hold
	// the startup for minutes. It reaches pgx as a duration, not as
	// connect_timeout in the connection string, which takes whole seconds.
	ConnectTimeout time.Duration `env:"POSTGRES_CONNECT_TIMEOUT" envDefault:"5s"`
}

// sslModes are the libpq sslmode values.
var sslModes = []string{"disable", "allow", "prefer", "require", "verify-ca", "verify-full"}

// ConnString returns a postgres:// URL for pgx. The user and the password are
// escaped, so reserved URL characters in them survive (design §7).
func (p Postgres) ConnString() string {
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(p.User, p.Password.Reveal()),
		Host:     net.JoinHostPort(p.Host, strconv.Itoa(p.Port)),
		Path:     "/" + p.Database,
		RawQuery: url.Values{"sslmode": {p.SSLMode}}.Encode(),
	}
	return u.String()
}

func (p Postgres) validate() []error {
	var errs []error

	if !validPort(p.Port) {
		errs = append(errs, problem("POSTGRES_PORT", "%d must be between 1 and 65535", p.Port))
	}
	if p.MaxConns <= 0 {
		errs = append(errs, problem("POSTGRES_MAX_CONNS", "%d must be positive", p.MaxConns))
	}
	if p.ConnectTimeout <= 0 {
		errs = append(errs, problem("POSTGRES_CONNECT_TIMEOUT", "%v must be positive", p.ConnectTimeout))
	}
	if !slices.Contains(sslModes, p.SSLMode) {
		errs = append(errs, problem("POSTGRES_SSLMODE",
			"%q must be one of %s", p.SSLMode, strings.Join(sslModes, ", ")))
	}

	return errs
}
