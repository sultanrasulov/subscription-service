package config

import (
	"net"
	"strconv"
	"time"
)

// HTTP configures the HTTP server.
type HTTP struct {
	Addr string `env:"HTTP_ADDR" envDefault:":8080"`

	// ReadHeaderTimeout bounds how long a client may take to send the
	// headers. Without it a server is open to slow-header attacks
	// (Slowloris), and gosec reports it (G112).
	ReadHeaderTimeout time.Duration `env:"HTTP_READ_HEADER_TIMEOUT" envDefault:"5s"`
	ReadTimeout       time.Duration `env:"HTTP_READ_TIMEOUT"        envDefault:"10s"`
	WriteTimeout      time.Duration `env:"HTTP_WRITE_TIMEOUT"       envDefault:"10s"`
	IdleTimeout       time.Duration `env:"HTTP_IDLE_TIMEOUT"        envDefault:"60s"`

	// RequestTimeout is the deadline of each request's context. It must be
	// shorter than WriteTimeout, so the client receives a problem+json
	// answer before the server closes the connection.
	RequestTimeout time.Duration `env:"HTTP_REQUEST_TIMEOUT" envDefault:"8s"`

	// ShutdownTimeout is how long graceful shutdown waits for in-flight
	// requests. It must be at least RequestTimeout, so shutdown never cuts
	// off a request that is still within its deadline.
	ShutdownTimeout time.Duration `env:"HTTP_SHUTDOWN_TIMEOUT" envDefault:"10s"`
}

func (h HTTP) validate() []error {
	var errs []error

	if _, port, err := net.SplitHostPort(h.Addr); err != nil {
		errs = append(errs, problem("HTTP_ADDR", "%q must be host:port, for example :8080", h.Addr))
	} else if n, err := strconv.Atoi(port); err != nil || !validPort(n) {
		errs = append(errs, problem("HTTP_ADDR", "%q must have a port between 1 and 65535", h.Addr))
	}

	for _, d := range []struct {
		variable string
		value    time.Duration
	}{
		{"HTTP_READ_HEADER_TIMEOUT", h.ReadHeaderTimeout},
		{"HTTP_READ_TIMEOUT", h.ReadTimeout},
		{"HTTP_WRITE_TIMEOUT", h.WriteTimeout},
		{"HTTP_IDLE_TIMEOUT", h.IdleTimeout},
		{"HTTP_REQUEST_TIMEOUT", h.RequestTimeout},
		{"HTTP_SHUTDOWN_TIMEOUT", h.ShutdownTimeout},
	} {
		if d.value <= 0 {
			errs = append(errs, problem(d.variable, "%v must be positive", d.value))
		}
	}

	if h.RequestTimeout >= h.WriteTimeout {
		errs = append(errs, problem("HTTP_REQUEST_TIMEOUT",
			"%v must be shorter than HTTP_WRITE_TIMEOUT (%v)", h.RequestTimeout, h.WriteTimeout))
	}
	if h.ShutdownTimeout < h.RequestTimeout {
		errs = append(errs, problem("HTTP_SHUTDOWN_TIMEOUT",
			"%v must not be shorter than HTTP_REQUEST_TIMEOUT (%v)", h.ShutdownTimeout, h.RequestTimeout))
	}

	return errs
}
