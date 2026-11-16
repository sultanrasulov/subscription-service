// Package jsonbody reads a request body that must be exactly one JSON object,
// strictly: the content type, the size, the structure and duplicate keys.
//
// It returns each field's raw value and leaves typing and validation to the
// caller, which knows the fields. It knows nothing about any particular
// request.
package jsonbody

import (
	"encoding/json"
	"errors"
	"io"
	"maps"
	"mime"
	"net/http"
	"slices"
)

// Object is a JSON object read from a request body.
type Object struct {
	// Fields holds each key's raw value, as it came. For a key that appears
	// more than once, it holds the first value.
	Fields map[string]json.RawMessage

	// Duplicates lists, sorted, each key that appears more than once.
	Duplicates []string
}

// Error is a body that cannot be read as one JSON object: the status to
// answer with, a detail for the client, and the cause, if any.
type Error struct {
	Status int
	Detail string
	Err    error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return e.Detail + ": " + e.Err.Error()
	}
	return e.Detail
}

// Unwrap returns the cause.
func (e *Error) Unwrap() error { return e.Err }

// Read reads r's body as one JSON object. Every error it returns is an
// *Error:
//   - a Content-Type other than application/json, or none: 415;
//   - a body over maxBytes: 413;
//   - a body that is not exactly one JSON object, including an empty body,
//     malformed JSON, and data after the object: 400;
//   - a body that cannot be read at all: 400. The server only reads what the
//     client sends, so a broken chunked encoding or a client too slow for the
//     read timeout is the client's fault, not the server's. The cause is
//     kept in Err.
//
// A key is compared with its escapes decoded, so "pri\u0063e" duplicates
// "price". RFC 8259 leaves duplicates undefined, and parsers that keep
// different copies of one can be played against each other, so every
// duplicate is reported to the caller.
func Read(w http.ResponseWriter, r *http.Request, maxBytes int64) (Object, error) {
	// Parameters such as charset are allowed, and the type is case-insensitive.
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return Object{}, &Error{Status: http.StatusUnsupportedMediaType, Detail: "Content-Type must be application/json"}
	}

	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes))
	start, err := dec.Token()
	switch {
	case errors.Is(err, io.EOF):
		return Object{}, notAnObject()
	case err != nil:
		return Object{}, readError(err)
	case start != json.Delim('{'):
		return Object{}, notAnObject()
	}

	obj := Object{Fields: make(map[string]json.RawMessage)}
	duplicates := make(map[string]struct{})
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return Object{}, readError(err)
		}
		// Inside an object the decoder only returns a key here.
		key, _ := token.(string)
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return Object{}, readError(err)
		}
		if _, seen := obj.Fields[key]; seen {
			duplicates[key] = struct{}{}
			continue
		}
		obj.Fields[key] = value
	}
	// The closing brace.
	if _, err := dec.Token(); err != nil {
		return Object{}, readError(err)
	}

	// Only whitespace may follow the object. Reading on also enforces the
	// size limit on whatever comes after it.
	if err := trailingError(dec); err != nil {
		return Object{}, err
	}

	obj.Duplicates = slices.Sorted(maps.Keys(duplicates))
	return obj, nil
}

// trailingError reads past the object and reports anything but the end of the
// body: another value, even a malformed one, is data after the object, while
// a body over the limit or one that cannot be read is reported as such.
func trailingError(dec *json.Decoder) error {
	_, err := dec.Token()
	var syntax *json.SyntaxError
	switch {
	case errors.Is(err, io.EOF):
		return nil
	case err == nil, errors.As(err, &syntax), errors.Is(err, io.ErrUnexpectedEOF):
		return &Error{Status: http.StatusBadRequest, Detail: "request body has data after the JSON object"}
	default:
		return readError(err)
	}
}

// readError classifies an error from inside the object. A body that ends
// there is malformed JSON, not an empty one: the decoder reports it as a
// syntax error or as io.ErrUnexpectedEOF, depending on where it stops.
func readError(err error) error {
	var syntax *json.SyntaxError
	switch {
	case tooLarge(err):
		return tooLargeError()
	case errors.As(err, &syntax), errors.Is(err, io.ErrUnexpectedEOF):
		return &Error{Status: http.StatusBadRequest, Detail: "request body is not valid JSON"}
	default:
		return &Error{Status: http.StatusBadRequest, Detail: "request body could not be read", Err: err}
	}
}

// tooLarge reports a body over the limit; the decoder passes the reader's
// error on from whichever read crossed it.
func tooLarge(err error) bool {
	var maxBytes *http.MaxBytesError
	return errors.As(err, &maxBytes)
}

func tooLargeError() *Error {
	return &Error{Status: http.StatusRequestEntityTooLarge, Detail: "request body too large"}
}

func notAnObject() *Error {
	return &Error{Status: http.StatusBadRequest, Detail: "request body must be a JSON object"}
}
