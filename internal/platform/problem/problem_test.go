package problem_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/sultanrasulov/subscription-service/internal/platform/problem"
)

// decode decodes the recorded body into Details and also into a raw map, so a
// test can check both the values and which keys are present.
func decode(t *testing.T, rec *httptest.ResponseRecorder) (problem.Details, map[string]any) {
	t.Helper()

	var details problem.Details
	if err := json.Unmarshal(rec.Body.Bytes(), &details); err != nil {
		t.Fatalf("body is not problem details: %v\n%s", err, rec.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("body is not a JSON object: %v\n%s", err, rec.Body.String())
	}
	return details, raw
}

func TestWrite_WritesProblemDetails(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	problem.Write(rec, http.StatusNotFound, "subscription not found")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/problem+json")
	}
	details, raw := decode(t, rec)
	want := problem.Details{Type: "about:blank", Title: "Not Found", Status: 404, Detail: "subscription not found"}
	if !reflect.DeepEqual(details, want) {
		t.Errorf("body = %+v, want %+v", details, want)
	}
	if _, ok := raw["errors"]; ok {
		t.Errorf("body has an errors key without field errors: %s", rec.Body.String())
	}
}

func TestWrite_IncludesFieldErrorsInOrder(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	problem.Write(rec, http.StatusBadRequest, "request validation failed",
		problem.FieldError{Field: "price", Message: "must be an integer between 0 and 2147483647"},
		problem.FieldError{Field: "start_date", Message: "must be in MM-YYYY format"},
	)

	details, _ := decode(t, rec)
	if details.Title != "Bad Request" || details.Status != 400 || details.Detail != "request validation failed" {
		t.Errorf("body = %+v", details)
	}
	want := []problem.FieldError{
		{Field: "price", Message: "must be an integer between 0 and 2147483647"},
		{Field: "start_date", Message: "must be in MM-YYYY format"},
	}
	if !reflect.DeepEqual(details.Errors, want) {
		t.Errorf("errors = %+v, want %+v", details.Errors, want)
	}
}

func TestWrite_OmitsEmptyDetail(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	problem.Write(rec, http.StatusMethodNotAllowed, "")

	_, raw := decode(t, rec)
	if _, ok := raw["detail"]; ok {
		t.Errorf("body has a detail key for an empty detail: %s", rec.Body.String())
	}
}

func TestWrite_SetsHeadersLikeHTTPError(t *testing.T) {
	t.Parallel()

	// The router sets Allow on 405 before the problem is written (design §4.4),
	// and a handler may have set Content-Length for a body it never sent.
	rec := httptest.NewRecorder()
	rec.Header().Set("Allow", "GET, HEAD")
	rec.Header().Set("Content-Length", "2")
	problem.Write(rec, http.StatusMethodNotAllowed, "method not allowed")

	if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
		t.Errorf("Allow = %q, want %q", got, "GET, HEAD")
	}
	if got := rec.Header().Get("Content-Length"); got != "" {
		t.Errorf("Content-Length = %q, want it removed", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
}

// A Content-Length left from another body makes a real server cut the problem
// off, so the client gets the status, an empty body and unexpected EOF.
func TestWrite_StaleContentLengthDoesNotCutTheBody(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "2")
		problem.Write(w, http.StatusInternalServerError, "internal server error")
	}))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatalf("GET error = %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("closing the body: %v", err)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	if !strings.Contains(string(body), `"detail":"internal server error"`) {
		t.Errorf("body = %q, want the whole problem details", body)
	}
}

func TestWrite_MembersInRFCOrder(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	problem.Write(rec, http.StatusBadRequest, "request validation failed",
		problem.FieldError{Field: "price", Message: "invalid"})

	body := rec.Body.String()
	keys := []string{`"type"`, `"title"`, `"status"`, `"detail"`, `"errors"`}
	last := -1
	for _, k := range keys {
		i := strings.Index(body, k)
		if i < 0 {
			t.Fatalf("member %s is missing: %s", k, body)
		}
		if i < last {
			t.Fatalf("members are not in the order %v: %s", keys, body)
		}
		last = i
	}
}
