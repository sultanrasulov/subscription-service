package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestDelete_RemovesTheSubscription(t *testing.T) {
	t.Parallel()

	e := newEnv()
	url, _ := e.created(t)

	w := e.do(t.Context(), http.MethodDelete, url, "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body %s", w.Code, w.Body)
	}
	if w.Body.Len() != 0 {
		t.Errorf("body = %q, want none", w.Body)
	}

	// Gone: reading it or deleting it again finds nothing.
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		if w := e.do(t.Context(), method, url, ""); w.Code != http.StatusNotFound {
			t.Errorf("%s after DELETE status = %d, want 404", method, w.Code)
		}
	}
}

func TestDelete_ReportsAMissingSubscription(t *testing.T) {
	t.Parallel()

	w := newEnv().do(t.Context(), http.MethodDelete, "/api/v1/subscriptions/"+uuid.Must(uuid.NewV7()).String(), "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body %s", w.Code, w.Body)
	}
	if p := problemOf(t, w); p.Detail != "subscription not found" {
		t.Errorf("detail = %q, want subscription not found", p.Detail)
	}
}

func TestDelete_RejectsAnInvalidID(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, id, message string
	}{
		{"not a UUID", "abc", msgUUID},
		{"as a URN", "urn:uuid:" + validUserID, msgUUID},
		{"nil", "00000000-0000-0000-0000-000000000000", msgNilUUID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			e := newEnv()
			url, before := e.created(t)
			w := e.do(t.Context(), http.MethodDelete, "/api/v1/subscriptions/"+tc.id, "")
			wantValidationErrors(t, w, errs("id", tc.message))
			if stored := e.do(t.Context(), http.MethodGet, url, ""); stored.Body.String() != before {
				t.Errorf("GET after a rejected DELETE = %s, want it unchanged", stored.Body)
			}
		})
	}
}
