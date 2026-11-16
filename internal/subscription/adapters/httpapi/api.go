// Package httpapi is the driving adapter that serves the subscription API over
// HTTP and JSON. It reads and validates raw input, calls the application, and
// turns the outcome into a response or an RFC 9457 problem.
//
// Its handlers carry the Swagger annotations the API's specification is
// generated from.
package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/sultanrasulov/subscription-service/internal/subscription/application"
)

const (
	// basePath prefixes every route of the API.
	basePath = "/api/v1"

	// maxBodyBytes is the largest request body read; a larger one is a 413.
	maxBodyBytes = 1 << 20
)

// API serves the subscription endpoints.
type API struct {
	svc *application.Service
	log *slog.Logger
}

// New returns the API over svc. It logs to log the errors that end a request:
// a server fault at ERROR, an expired deadline at WARN.
func New(svc *application.Service, log *slog.Logger) *API {
	return &API{svc: svc, log: log}
}

// Register adds the API's routes to mux.
func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST "+basePath+"/subscriptions", a.create)
	mux.HandleFunc("GET "+basePath+"/subscriptions/{id}", a.get)
}
