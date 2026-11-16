// Package smoke_test checks a running stack end to end: the built image, its
// migrations and a real database, through the public API only, as a client
// does. It imports none of the service's code.
//
// make smoke runs it against the stack make up started; CI runs it the same
// way. Without SMOKE_BASE_URL it skips, so go test ./... needs no stack.
package smoke_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// envBaseURL names the variable that holds the stack's address, such as
// http://127.0.0.1:8080.
const envBaseURL = "SMOKE_BASE_URL"

// client fails a request to a hung stack instead of waiting for the CI job's
// own timeout.
var client = &http.Client{Timeout: 10 * time.Second}

// baseURL returns the stack's address. The test skips only when the variable
// is not set at all. go test counts a skip as a pass, so a value that is set
// but unusable fails the test: a check that skipped by mistake would leave CI
// green without checking anything. os.Getenv cannot tell an unset variable
// from an empty one; os.LookupEnv can.
func baseURL(t *testing.T) *url.URL {
	t.Helper()
	raw, ok := os.LookupEnv(envBaseURL)
	if !ok {
		t.Skipf("%s is not set, so there is no stack to check", envBaseURL)
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		t.Fatalf("%s = %q, want an absolute http or https URL", envBaseURL, raw)
	}
	return u
}

func TestSmoke_OperationalRoutes(t *testing.T) {
	base := baseURL(t)

	for _, path := range []string{"/health", "/ready", "/swagger/index.html"} {
		if status, body := send(t, http.MethodGet, base, path, ""); status != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200; body %s", path, status, body)
		}
	}

	status, body := send(t, http.MethodGet, base, "/swagger/doc.json", "")
	if status != http.StatusOK {
		t.Fatalf("GET /swagger/doc.json status = %d, want 200; body %s", status, body)
	}
	var spec struct {
		Swagger  string                     `json:"swagger"`
		BasePath string                     `json:"basePath"`
		Paths    map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(body, &spec); err != nil {
		t.Fatalf("decode the specification: %v", err)
	}
	if spec.Swagger != "2.0" || spec.BasePath != "/api/v1" {
		t.Errorf("specification = Swagger %q with base path %q, want Swagger 2.0 with /api/v1", spec.Swagger, spec.BasePath)
	}
	missing, unexpected := compare(wantOperations, operations(t, spec.Paths))
	if len(missing) > 0 || len(unexpected) > 0 {
		t.Errorf("documented operations: missing %q, unexpected %q", missing, unexpected)
	}
}

// wantOperations are the operations the API serves. swag leaves out a handler
// whose @Router line is lost, without a warning, so only a check of the built
// specification notices an operation leaving it.
var wantOperations = []string{
	"POST /subscriptions",
	"GET /subscriptions",
	"GET /subscriptions/total-cost",
	"GET /subscriptions/{id}",
	"PUT /subscriptions/{id}",
	"DELETE /subscriptions/{id}",
}

// operationKeys are the keys of a Swagger 2.0 path item that name an operation.
// Its other keys, parameters, $ref and x- extensions, are not operations.
var operationKeys = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true, "options": true, "head": true, "patch": true,
}

// operations lists a specification's operations as "METHOD path". Only a key
// that starts with "/" is a path: the paths object may also hold x- extensions.
func operations(t *testing.T, paths map[string]json.RawMessage) []string {
	t.Helper()
	var ops []string
	for path, raw := range paths {
		if !strings.HasPrefix(path, "/") {
			continue
		}
		var item map[string]json.RawMessage
		if err := json.Unmarshal(raw, &item); err != nil {
			t.Fatalf("decode the path item %s: %v", path, err)
		}
		for key := range item {
			if operationKeys[key] {
				ops = append(ops, strings.ToUpper(key)+" "+path)
			}
		}
	}
	return ops
}

// compare returns what want has and got lacks, and what got has and want
// lacks, each sorted.
func compare(want, got []string) (missing, unexpected []string) {
	for _, s := range want {
		if !slices.Contains(got, s) {
			missing = append(missing, s)
		}
	}
	for _, s := range got {
		if !slices.Contains(want, s) {
			unexpected = append(unexpected, s)
		}
	}
	slices.Sort(missing)
	slices.Sort(unexpected)
	return missing, unexpected
}

// A subscription created for a user of its own reads back through its
// Location, and its total cost comes from the reader the service wires. The
// user is new on every run, so the test can run again on the same stack.
func TestSmoke_SubscriptionEndToEnd(t *testing.T) {
	base := baseURL(t)
	user := uuid.New().String()

	resp := do(t, http.MethodPost, base, "/api/v1/subscriptions",
		fmt.Sprintf(`{"service_name": "Yandex Plus", "price": 400, "user_id": %q, "start_date": "07-2025"}`, user))
	created := read(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST status = %d, want exactly 201; body %s", resp.StatusCode, created)
	}
	location := resp.Header.Get("Location")
	if location == "" {
		t.Fatal("POST response has no Location")
	}

	status, body := send(t, http.MethodGet, base, location, "")
	if status != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200; body %s", location, status, body)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode the subscription: %v", err)
	}
	if _, hasEnd := got["end_date"]; got["service_name"] != "Yandex Plus" || got["price"] != 400.0 ||
		got["user_id"] != user || got["start_date"] != "07-2025" || hasEnd {
		t.Errorf("subscription = %v, want Yandex Plus at 400 for %s from 07-2025, open-ended", got, user)
	}

	path := "/api/v1/subscriptions/total-cost?from=01-2025&to=12-2025&user_id=" + user
	status, body = send(t, http.MethodGet, base, path, "")
	if status != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200; body %s", path, status, body)
	}
	var total struct {
		TotalCost *int64 `json:"total_cost"`
	}
	if err := json.Unmarshal(body, &total); err != nil {
		t.Fatalf("decode the total cost: %v", err)
	}
	const want = 6 * 400 // open-ended from 07-2025 at 400: six months of 2025
	if total.TotalCost == nil || *total.TotalCost != want {
		t.Errorf("total cost = %s, want {\"total_cost\":%d}", bytes.TrimSpace(body), want)
	}
}

// send makes a request and returns its status and body.
func send(t *testing.T, method string, base *url.URL, target, body string) (int, []byte) {
	t.Helper()
	resp := do(t, method, base, target, body)
	return resp.StatusCode, read(t, resp)
}

// do makes a request to target, a path with an optional query, resolved
// against base. A body is sent as JSON.
func do(t *testing.T, method string, base *url.URL, target, body string) *http.Response {
	t.Helper()
	ref, err := url.Parse(target)
	if err != nil {
		t.Fatalf("parse %q: %v", target, err)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, base.ResolveReference(ref).String(), strings.NewReader(body))
	if err != nil {
		t.Fatalf("build %s %s: %v", method, target, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	return resp
}

// read returns a response's body and closes it.
func read(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if closeErr := resp.Body.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatalf("read the response: %v", err)
	}
	return body
}
