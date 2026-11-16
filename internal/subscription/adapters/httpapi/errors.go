package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/sultanrasulov/subscription-service/internal/platform/jsonbody"
	"github.com/sultanrasulov/subscription-service/internal/platform/problem"
	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
)

// invalid answers a request whose fields failed validation.
func invalid(w http.ResponseWriter, fieldErrors []problem.FieldError) {
	problem.Write(w, http.StatusBadRequest, "request validation failed", fieldErrors...)
}

// unreadable answers a request whose body could not be read as one JSON
// object, with the status and detail the reader chose.
func (a *API) unreadable(w http.ResponseWriter, r *http.Request, err error) {
	var bodyErr *jsonbody.Error
	if errors.As(err, &bodyErr) {
		problem.Write(w, bodyErr.Status, bodyErr.Detail)
		return
	}
	a.fail(w, r, "read the request body", err)
}

// fail answers a request whose call to the application failed. The request
// was validated before the call, so the status follows from the error alone:
//   - the request context's deadline expired: 503, and a warning naming the
//     operation that was cut off;
//   - the client went away: nothing to answer, and nothing to log, because
//     the request log line records it;
//   - the subscription does not exist: 404;
//   - anything else is a server fault: 500, and the whole error logged once.
//     That includes the domain's "invalid data", which valid input cannot
//     produce there: it comes from a stored record that breaks an invariant.
//
// The context decides the first two, not the error chain, so they do not
// depend on how the database driver wraps a context's error.
func (a *API) fail(w http.ResponseWriter, r *http.Request, op string, err error) {
	ctx := r.Context()
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		a.log.WarnContext(ctx, "request timed out", "operation", op, "error", err)
		problem.Write(w, http.StatusServiceUnavailable, "request timed out")
	case errors.Is(ctx.Err(), context.Canceled):
	case errors.Is(err, domain.ErrNotFound):
		problem.Write(w, http.StatusNotFound, "subscription not found")
	default:
		a.log.ErrorContext(ctx, "request failed", "operation", op, "error", err)
		problem.Write(w, http.StatusInternalServerError, "internal server error")
	}
}
