package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// updateBody returns a valid update body, in the schema's field order, with
// each field in replace set to its raw JSON value instead, or dropped when the
// raw value is empty.
func updateBody(replace map[string]string) string {
	order := []string{"service_name", "price", "start_date", "end_date"}
	valid := map[string]string{
		"service_name": `"Kinopoisk"`,
		"price":        `299`,
		"start_date":   `"01-2026"`,
		"end_date":     `"03-2026"`,
	}
	var parts []string
	for _, k := range order {
		v, ok := replace[k]
		if !ok {
			v = valid[k]
		}
		if v != "" {
			parts = append(parts, fmt.Sprintf("%q: %s", k, v))
		}
	}
	for k, v := range replace {
		if _, ok := valid[k]; !ok {
			parts = append(parts, fmt.Sprintf("%q: %s", k, v))
		}
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// created stores a subscription through the API and returns its URL and body.
func (e env) created(t *testing.T) (string, string) {
	t.Helper()
	w := e.do(t.Context(), http.MethodPost, "/api/v1/subscriptions", createBody(nil))
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d; body %s", w.Code, w.Body)
	}
	return w.Header().Get("Location"), w.Body.String()
}

func TestUpdate_ReplacesTheSubscription(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		replace map[string]string
		wantEnd string // "" when open-ended
	}{
		{"closed period", nil, "03-2026"},
		{"to open-ended", map[string]string{"end_date": ""}, ""},
		{"to open-ended with null", map[string]string{"end_date": "null"}, ""},
		{"to a free trial", map[string]string{"price": "0"}, "03-2026"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			e := newEnv()
			url, before := e.created(t)
			w := e.do(t.Context(), http.MethodPut, url, updateBody(tc.replace))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body %s", w.Code, w.Body)
			}

			var was, got map[string]any
			if err := json.Unmarshal([]byte(before), &was); err != nil {
				t.Fatalf("decode the created subscription: %v", err)
			}
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode the body: %v", err)
			}
			wantPrice := 299.0
			if tc.replace["price"] == "0" {
				wantPrice = 0
			}
			if got["id"] != was["id"] || got["user_id"] != was["user_id"] {
				t.Errorf("id, user = %v, %v; want them unchanged, %v, %v", got["id"], got["user_id"], was["id"], was["user_id"])
			}
			if got["service_name"] != "Kinopoisk" || got["price"] != wantPrice || got["start_date"] != "01-2026" {
				t.Errorf("body = %v, want the new name, price and start", got)
			}
			end, hasEnd := got["end_date"]
			switch {
			case tc.wantEnd == "" && hasEnd:
				t.Errorf("end_date = %v, want it omitted for an open-ended subscription", end)
			case tc.wantEnd != "" && end != tc.wantEnd:
				t.Errorf("end_date = %v, want %s", end, tc.wantEnd)
			}

			// The change is stored, not only answered.
			if stored := e.do(t.Context(), http.MethodGet, url, ""); stored.Body.String() != w.Body.String() {
				t.Errorf("GET after PUT = %s, want %s", stored.Body, w.Body)
			}
		})
	}
}

// The user of a subscription never changes, so the update body has no
// user_id: one sent anyway is an unknown field, and nothing is stored.
func TestUpdate_RejectsAUserID(t *testing.T) {
	t.Parallel()

	e := newEnv()
	url, before := e.created(t)
	w := e.do(t.Context(), http.MethodPut, url, updateBody(map[string]string{"user_id": `"` + otherUserID + `"`}))
	wantValidationErrors(t, w, errs("user_id", msgUnknown))

	if stored := e.do(t.Context(), http.MethodGet, url, ""); stored.Body.String() != before {
		t.Errorf("GET after a rejected PUT = %s, want it unchanged, %s", stored.Body, before)
	}
}

// The body is read through the same rules as create's.
func TestUpdate_RejectsAnInvalidBody(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		body   string
		status int
		detail string
		errors string
	}{
		{"null price", updateBody(map[string]string{"price": "null"}), http.StatusBadRequest, "request validation failed", "price: " + msgRequired},
		{"missing start", updateBody(map[string]string{"start_date": ""}), http.StatusBadRequest, "request validation failed", "start_date: " + msgRequired},
		{"end before start", updateBody(map[string]string{"end_date": `"12-2025"`}), http.StatusBadRequest, "request validation failed", "end_date: " + msgEndBeforeStart},
		{"duplicate field", `{"service_name": "Okko", "service_name": "Ivi", "price": 1, "start_date": "01-2026"}`, http.StatusBadRequest, "request validation failed", "service_name: " + msgDuplicate},
		{"array", `[]`, http.StatusBadRequest, "request body must be a JSON object", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			e := newEnv()
			url, before := e.created(t)
			w := e.do(t.Context(), http.MethodPut, url, tc.body)
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d; body %s", w.Code, tc.status, w.Body)
			}
			p := problemOf(t, w)
			var got []string
			for _, fe := range p.Errors {
				got = append(got, fe.Field+": "+fe.Message)
			}
			if p.Detail != tc.detail || strings.Join(got, "; ") != tc.errors {
				t.Errorf("problem = %q %v, want %q [%s]", p.Detail, got, tc.detail, tc.errors)
			}
			if stored := e.do(t.Context(), http.MethodGet, url, ""); stored.Body.String() != before {
				t.Errorf("GET after a rejected PUT = %s, want it unchanged", stored.Body)
			}
		})
	}
}

func TestUpdate_RejectsTheWrongContentType(t *testing.T) {
	t.Parallel()

	e := newEnv()
	url, _ := e.created(t)
	r := newRequest(t, http.MethodPut, url, updateBody(nil), "text/plain")
	w := e.serve(r)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status = %d, want 415; body %s", w.Code, w.Body)
	}
}

func TestUpdate_ReportsAMissingSubscription(t *testing.T) {
	t.Parallel()

	w := newEnv().do(t.Context(), http.MethodPut, "/api/v1/subscriptions/"+uuid.Must(uuid.NewV7()).String(), updateBody(nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body %s", w.Code, w.Body)
	}
	if p := problemOf(t, w); p.Detail != "subscription not found" {
		t.Errorf("detail = %q, want subscription not found", p.Detail)
	}
}

// The path names the resource, so a malformed ID is answered alone: the body
// is not read, as there is nothing to apply it to.
func TestUpdate_RejectsAnInvalidID(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, id, message string
	}{
		{"not a UUID", "abc", msgUUID},
		{"nil", "00000000-0000-0000-0000-000000000000", msgNilUUID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := newEnv().do(t.Context(), http.MethodPut, "/api/v1/subscriptions/"+tc.id, `[]`)
			wantValidationErrors(t, w, errs("id", tc.message))
		})
	}
}
