// Package problem writes RFC 9457 problem details (application/problem+json).
//
// It is the one implementation of the service's error format (design §4.4,
// decision #57): the HTTP server uses it for router errors and panics, and the
// HTTP adapter for validation errors, "not found" and internal errors. It knows
// nothing about subscriptions.
package problem

import (
	"encoding/json"
	"net/http"
)

// ContentType is the media type of a problem details body (RFC 9457 §3).
const ContentType = "application/problem+json"

// FieldError is one invalid request field. It is an extension member of the
// problem details object, under "errors".
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Details is an RFC 9457 problem details object.
//
// Type is always "about:blank", so Title is the HTTP status phrase
// (RFC 9457 §4.2.1). The members are declared in the order RFC 9457 lists
// them, which is the order encoding/json writes them.
type Details struct {
	Type   string       `json:"type"`
	Title  string       `json:"title"`
	Status int          `json:"status"`
	Detail string       `json:"detail,omitempty"`
	Errors []FieldError `json:"errors,omitempty"`
}

// Write writes a problem details response with the given status, detail and
// field errors, in that order. Headers set on w before the call, such as
// Allow, are kept.
//
// Write is the last thing a handler does: it sends the status line.
func Write(w http.ResponseWriter, status int, detail string, fieldErrors ...FieldError) {
	// As http.Error does: a Content-Length set before the call describes some
	// other body and would cut this one off; nosniff keeps browsers from
	// guessing another content type. Content-Encoding is left alone, because
	// compression middleware sets it and compresses the body itself
	// (go.dev/issue/66343).
	h := w.Header()
	h.Del("Content-Length")
	h.Set("Content-Type", ContentType)
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)

	// Once the status line is sent, an encoding or write error cannot reach
	// the client, and the caller has nothing left to do with it.
	_ = json.NewEncoder(w).Encode(Details{
		Type:   "about:blank",
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
		Errors: fieldErrors,
	})
}
