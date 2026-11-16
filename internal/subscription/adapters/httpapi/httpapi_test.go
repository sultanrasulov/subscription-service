package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sultanrasulov/subscription-service/internal/platform/problem"
	"github.com/sultanrasulov/subscription-service/internal/subscription/adapters/httpapi"
	"github.com/sultanrasulov/subscription-service/internal/subscription/application"
	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
	"github.com/sultanrasulov/subscription-service/internal/subscription/domain/domaintest"
)

// env is the API over the real application service and a fake repository,
// with the adapter's log records in memory.
type env struct {
	repo    *domaintest.Repository
	handler http.Handler
	logs    *recorder
}

func newEnv() env {
	repo := domaintest.NewRepository()
	logs := &recorder{}
	svc := application.NewService(repo, domaintest.NewTotalCostReader(repo), slog.New(slog.DiscardHandler))
	mux := http.NewServeMux()
	httpapi.New(svc, slog.New(logs)).Register(mux)
	return env{repo: repo, handler: mux, logs: logs}
}

// do sends a request with a JSON content type and returns the response.
func (e env) do(ctx context.Context, method, target, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequestWithContext(ctx, method, target, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.handler.ServeHTTP(w, r)
	return w
}

// newRequest builds a request with the given content type.
func newRequest(t *testing.T, method, target, body, contentType string) *http.Request {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
	r.Header.Set("Content-Type", contentType)
	return r
}

// serve runs a request through the API.
func (e env) serve(r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	e.handler.ServeHTTP(w, r)
	return w
}

// problemOf decodes a problem+json response.
func problemOf(t *testing.T, w *httptest.ResponseRecorder) problem.Details {
	t.Helper()
	if ct := w.Header().Get("Content-Type"); ct != problem.ContentType {
		t.Fatalf("Content-Type = %q, want %q; body %s", ct, problem.ContentType, w.Body)
	}
	var p problem.Details
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode the problem: %v; body %s", err, w.Body)
	}
	return p
}

// recorder is a slog.Handler that keeps every record.
type recorder struct {
	mu      sync.Mutex
	records []slog.Record
}

func (r *recorder) Enabled(context.Context, slog.Level) bool { return true }

func (r *recorder) Handle(_ context.Context, record slog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, record.Clone())
	return nil
}

func (r *recorder) WithAttrs([]slog.Attr) slog.Handler { return r }

func (r *recorder) WithGroup(string) slog.Handler { return r }

// all returns every record, each as its level, message and attributes.
func (r *recorder) all() []map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []map[string]string
	for _, record := range r.records {
		e := map[string]string{"level": record.Level.String(), "msg": record.Message}
		record.Attrs(func(a slog.Attr) bool {
			e[a.Key] = a.Value.String()
			return true
		})
		out = append(out, e)
	}
	return out
}

const (
	validUserID = "60601fee-2bf1-4721-ae6f-7636e79a0cba"

	msgRequired       = "is required"
	msgUnknown        = "unknown field"
	msgDuplicate      = "duplicate field"
	msgString         = "must be a string"
	msgEmpty          = "must not be empty"
	msgTooLong        = "must be at most 100 characters"
	msgCharacters     = "must not contain control characters or invalid UTF-8"
	msgPrice          = "must be an integer between 0 and 2147483647"
	msgUUID           = "must be a valid UUID"
	msgNilUUID        = "must not be the nil UUID"
	msgMonthFormat    = "must be in MM-YYYY format"
	msgMonth          = "month must be between 01 and 12"
	msgYear           = "year must be between 2000 and 2100"
	msgEndBeforeStart = "must not be before start_date"
)

// createBody returns a valid create body, in the schema's field order, with
// each field in replace set to its raw JSON value instead, or dropped when the
// raw value is empty. Keys outside the schema are added after it, sorted.
func createBody(replace map[string]string) string {
	order := []string{"service_name", "price", "user_id", "start_date", "end_date"}
	valid := map[string]string{
		"service_name": `"Yandex Plus"`,
		"price":        `400`,
		"user_id":      `"` + validUserID + `"`,
		"start_date":   `"07-2025"`,
		"end_date":     `"12-2025"`,
	}
	var extra []string
	for k := range replace {
		if _, ok := valid[k]; !ok {
			extra = append(extra, k)
		}
	}
	slices.Sort(extra)

	var parts []string
	for _, k := range append(order, extra...) {
		v, ok := replace[k]
		if !ok {
			v = valid[k]
		}
		if v == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%q: %s", k, v))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func TestCreate_StoresTheSubscription(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		replace map[string]string
		wantEnd string // "" when open-ended
	}{
		{"closed period", nil, "12-2025"},
		{"no end date", map[string]string{"end_date": ""}, ""},
		{"null end date", map[string]string{"end_date": "null"}, ""},
		{"one month", map[string]string{"end_date": `"07-2025"`}, "07-2025"},
		{"free trial", map[string]string{"price": "0"}, "12-2025"},
		{"upper-case user ID", map[string]string{"user_id": `"` + strings.ToUpper(validUserID) + `"`}, "12-2025"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			e := newEnv()
			w := e.do(t.Context(), http.MethodPost, "/api/v1/subscriptions", createBody(tc.replace))
			if w.Code != http.StatusCreated {
				t.Fatalf("status = %d, want 201; body %s", w.Code, w.Body)
			}
			if ct := w.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			// As on every error, so a browser never sniffs another type.
			if v := w.Header().Get("X-Content-Type-Options"); v != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want nosniff", v)
			}

			var got map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode the body: %v", err)
			}
			id, _ := got["id"].(string)
			if parsed, err := uuid.Parse(id); err != nil || parsed.Version() != 7 || id != strings.ToLower(id) {
				t.Errorf("id = %q, want a lower-case UUID version 7", id)
			}
			if loc := w.Header().Get("Location"); loc != "/api/v1/subscriptions/"+id {
				t.Errorf("Location = %q, want /api/v1/subscriptions/%s", loc, id)
			}
			wantPrice := 400.0
			if tc.replace["price"] == "0" {
				wantPrice = 0
			}
			if got["service_name"] != "Yandex Plus" || got["price"] != wantPrice ||
				got["user_id"] != validUserID || got["start_date"] != "07-2025" {
				t.Errorf("body = %v, want the sent fields", got)
			}
			end, hasEnd := got["end_date"]
			switch {
			case tc.wantEnd == "" && hasEnd:
				t.Errorf("end_date = %v, want it omitted for an open-ended subscription", end)
			case tc.wantEnd != "" && end != tc.wantEnd:
				t.Errorf("end_date = %v, want %s", end, tc.wantEnd)
			}

			if _, total, err := e.repo.List(t.Context(), listAll()); err != nil || total != 1 {
				t.Errorf("stored subscriptions = %d, %v; want 1", total, err)
			}
		})
	}
}

func TestCreate_RejectsInvalidFields(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		replace map[string]string
		want    []problem.FieldError
	}{
		{"missing name", map[string]string{"service_name": ""}, errs("service_name", msgRequired)},
		{"null name", map[string]string{"service_name": "null"}, errs("service_name", msgRequired)},
		{"name not a string", map[string]string{"service_name": "400"}, errs("service_name", msgString)},
		{"empty name", map[string]string{"service_name": `""`}, errs("service_name", msgEmpty)},
		{"blank name", map[string]string{"service_name": `"   "`}, errs("service_name", msgEmpty)},
		{"name of 101 characters", map[string]string{"service_name": `"` + strings.Repeat("я", 101) + `"`}, errs("service_name", msgTooLong)},
		{"escape in the name", map[string]string{"service_name": `"Yandex\u001bPlus"`}, errs("service_name", msgCharacters)},

		{"missing price", map[string]string{"price": ""}, errs("price", msgRequired)},
		{"null price", map[string]string{"price": "null"}, errs("price", msgRequired)},
		{"fractional price", map[string]string{"price": "400.0"}, errs("price", msgPrice)},
		{"price with an exponent", map[string]string{"price": "4e2"}, errs("price", msgPrice)},
		{"price as a string", map[string]string{"price": `"400"`}, errs("price", msgPrice)},
		{"price as a boolean", map[string]string{"price": "true"}, errs("price", msgPrice)},
		{"negative price", map[string]string{"price": "-1"}, errs("price", msgPrice)},
		{"price over the maximum", map[string]string{"price": "2147483648"}, errs("price", msgPrice)},
		{"price over 64 bits", map[string]string{"price": "99999999999999999999"}, errs("price", msgPrice)},

		{"missing user", map[string]string{"user_id": ""}, errs("user_id", msgRequired)},
		{"user ID not a string", map[string]string{"user_id": "123"}, errs("user_id", msgUUID)},
		{"user ID not a UUID", map[string]string{"user_id": `"abc"`}, errs("user_id", msgUUID)},
		{"user ID in braces", map[string]string{"user_id": `"{` + validUserID + `}"`}, errs("user_id", msgUUID)},
		{"user ID as a URN", map[string]string{"user_id": `"urn:uuid:` + validUserID + `"`}, errs("user_id", msgUUID)},
		{"user ID without hyphens", map[string]string{"user_id": `"` + strings.ReplaceAll(validUserID, "-", "") + `"`}, errs("user_id", msgUUID)},
		{"nil user ID", map[string]string{"user_id": `"00000000-0000-0000-0000-000000000000"`}, errs("user_id", msgNilUUID)},

		{"missing start", map[string]string{"start_date": ""}, errs("start_date", msgRequired)},
		{"start not a string", map[string]string{"start_date": "72025"}, errs("start_date", msgMonthFormat)},
		{"start with one digit", map[string]string{"start_date": `"7-2025"`}, errs("start_date", msgMonthFormat)},
		{"start with a slash", map[string]string{"start_date": `"07/2025"`}, errs("start_date", msgMonthFormat)},
		{"start in ISO order", map[string]string{"start_date": `"2025-07"`}, errs("start_date", msgMonthFormat)},
		{"start with a two-digit year", map[string]string{"start_date": `"07-25"`}, errs("start_date", msgMonthFormat)},
		{"start with a trailing space", map[string]string{"start_date": `"07-2025 "`}, errs("start_date", msgMonthFormat)},
		{"start with a three-digit month", map[string]string{"start_date": `"007-2025"`}, errs("start_date", msgMonthFormat)},
		{"start with a five-digit year", map[string]string{"start_date": `"07-20255"`}, errs("start_date", msgMonthFormat)},
		{"start in month 13", map[string]string{"start_date": `"13-2025"`}, errs("start_date", msgMonth)},
		{"start in month 00", map[string]string{"start_date": `"00-2025"`}, errs("start_date", msgMonth)},
		{"start in 1999", map[string]string{"start_date": `"12-1999"`}, errs("start_date", msgYear)},
		{"start in 2101", map[string]string{"start_date": `"01-2101"`}, errs("start_date", msgYear)},

		{"end in month 13", map[string]string{"end_date": `"13-2025"`}, errs("end_date", msgMonth)},
		{"end before start", map[string]string{"end_date": `"06-2025"`}, errs("end_date", msgEndBeforeStart)},
		// An invalid start leaves nothing to compare the end with.
		{"invalid start, earlier end", map[string]string{"start_date": `"13-2025"`, "end_date": `"06-2025"`}, errs("start_date", msgMonth)},

		{"unknown field", map[string]string{"comment": `"x"`}, errs("comment", msgUnknown)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			e := newEnv()
			w := e.do(t.Context(), http.MethodPost, "/api/v1/subscriptions", createBody(tc.replace))
			wantValidationErrors(t, w, tc.want)
			if _, total, _ := e.repo.List(t.Context(), listAll()); total != 0 {
				t.Errorf("stored subscriptions = %d, want none", total)
			}
		})
	}
}

// The same mistakes give the same response, whatever the order of the keys.
// A duplicated field's value is not checked: it is unclear which copy counts.
func TestCreate_ReportsEveryErrorInAFixedOrder(t *testing.T) {
	t.Parallel()

	want := []problem.FieldError{
		{Field: "service_name", Message: msgString},
		{Field: "user_id", Message: msgUUID},
		{Field: "start_date", Message: msgMonthFormat},
		{Field: "end_date", Message: msgMonthFormat},
		{Field: "alpha", Message: msgUnknown},
		{Field: "delta", Message: msgUnknown},
		{Field: "kappa", Message: msgUnknown},
		{Field: "mu", Message: msgUnknown},
		{Field: "zeta", Message: msgUnknown},
		{Field: "beta", Message: msgDuplicate},
		{Field: "price", Message: msgDuplicate},
	}
	for name, body := range map[string]string{
		"one order": `{"zeta": 1, "mu": 1, "end_date": "x", "start_date": "x", "user_id": "x", "price": 1.5,
			"kappa": 1, "service_name": 1, "alpha": 1, "price": 2, "delta": 1, "beta": 1, "beta": 2}`,
		"another order": `{"beta": 1, "delta": 1, "price": 2, "alpha": 1, "service_name": 1, "price": 1.5,
			"user_id": "x", "beta": 2, "kappa": 1, "start_date": "x", "zeta": 1, "end_date": "x", "mu": 1}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			w := newEnv().do(t.Context(), http.MethodPost, "/api/v1/subscriptions", body)
			wantValidationErrors(t, w, want)
		})
	}
}

func TestCreate_RejectsTheBodyAsAWhole(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		contentType string
		body        string
		status      int
		detail      string
	}{
		{"empty", "application/json", "", http.StatusBadRequest, "request body must be a JSON object"},
		{"array", "application/json", `[]`, http.StatusBadRequest, "request body must be a JSON object"},
		{"data after the object", "application/json", createBody(nil) + `{}`, http.StatusBadRequest, "request body has data after the JSON object"},
		{"not JSON", "text/plain", createBody(nil), http.StatusUnsupportedMediaType, "Content-Type must be application/json"},
		{"over 1 MiB", "application/json", `{"service_name": "` + strings.Repeat("a", 1<<20) + `"}`, http.StatusRequestEntityTooLarge, "request body too large"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			e := newEnv()
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/subscriptions", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", tc.contentType)
			w := httptest.NewRecorder()
			e.handler.ServeHTTP(w, r)

			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d; body %s", w.Code, tc.status, w.Body)
			}
			p := problemOf(t, w)
			if p.Detail != tc.detail || len(p.Errors) != 0 {
				t.Errorf("problem = %q %v, want %q and no field errors", p.Detail, p.Errors, tc.detail)
			}
		})
	}
}

// A body that cannot be read is the client's 400. Its cause, such as a network
// error naming the server's address, stays out of the response.
func TestCreate_HidesWhyTheBodyCouldNotBeRead(t *testing.T) {
	t.Parallel()

	e := newEnv()
	body := io.MultiReader(strings.NewReader(`{"service_name": `), failingBody{})
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/subscriptions", body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body %s", w.Code, w.Body)
	}
	if p := problemOf(t, w); p.Detail != "request body could not be read" {
		t.Errorf("detail = %q, want exactly request body could not be read", p.Detail)
	}
	if strings.Contains(w.Body.String(), "10.0.0.1") {
		t.Errorf("body = %s, leaks the cause", w.Body)
	}
}

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) {
	return 0, errors.New("read tcp 10.0.0.1:8080->10.0.0.2:51234: i/o timeout")
}

func TestGet_ReturnsTheSubscription(t *testing.T) {
	t.Parallel()

	e := newEnv()
	created := e.do(t.Context(), http.MethodPost, "/api/v1/subscriptions", createBody(nil))
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d; body %s", created.Code, created.Body)
	}

	w := e.do(t.Context(), http.MethodGet, created.Header().Get("Location"), "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", w.Code, w.Body)
	}
	if w.Body.String() != created.Body.String() {
		t.Errorf("body = %s, want what create returned, %s", w.Body, created.Body)
	}
}

func TestGet_RejectsAnInvalidID(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, id, message string
	}{
		{"not a UUID", "abc", msgUUID},
		{"in braces", "%7B" + validUserID + "%7D", msgUUID},
		{"as a URN", "urn:uuid:" + validUserID, msgUUID},
		{"without hyphens", strings.ReplaceAll(validUserID, "-", ""), msgUUID},
		{"nil", "00000000-0000-0000-0000-000000000000", msgNilUUID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := newEnv().do(t.Context(), http.MethodGet, "/api/v1/subscriptions/"+tc.id, "")
			wantValidationErrors(t, w, errs("id", tc.message))
		})
	}
}

func TestGet_ReportsAMissingSubscription(t *testing.T) {
	t.Parallel()

	w := newEnv().do(t.Context(), http.MethodGet, "/api/v1/subscriptions/"+uuid.Must(uuid.NewV7()).String(), "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body %s", w.Code, w.Body)
	}
	if p := problemOf(t, w); p.Detail != "subscription not found" {
		t.Errorf("detail = %q, want subscription not found", p.Detail)
	}
}

// A store that outlives the request deadline gives a 503 and one warning
// naming the operation.
func TestErrors_DeadlineIsA503(t *testing.T) {
	t.Parallel()

	e := newEnv()
	e.repo.Block()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	w := e.do(ctx, http.MethodPost, "/api/v1/subscriptions", createBody(nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body %s", w.Code, w.Body)
	}
	if p := problemOf(t, w); p.Detail != "request timed out" {
		t.Errorf("detail = %q, want request timed out", p.Detail)
	}
	logs := e.logs.all()
	if len(logs) != 1 || logs[0]["level"] != "WARN" || logs[0]["operation"] == "" {
		t.Errorf("logs = %v, want one WARN naming the operation", logs)
	}
}

// An "invalid data" error from the application cannot come from the request,
// which was validated before the call; it comes from a stored record that
// breaks an invariant, so it is a server fault.
func TestErrors_InvalidDataFromTheStoreIsA500(t *testing.T) {
	t.Parallel()

	e := newEnv()
	e.repo.Fail(fmt.Errorf("invalid stored row: %w", errInvalid))

	w := e.do(t.Context(), http.MethodGet, "/api/v1/subscriptions/"+uuid.Must(uuid.NewV7()).String(), "")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body %s", w.Code, w.Body)
	}
	if p := problemOf(t, w); p.Detail != "internal server error" || len(p.Errors) != 0 {
		t.Errorf("problem = %q %v, want internal server error with no internals", p.Detail, p.Errors)
	}
	logs := e.logs.all()
	if len(logs) != 1 || logs[0]["level"] != "ERROR" || !strings.Contains(logs[0]["error"], "invalid stored row") {
		t.Errorf("logs = %v, want one ERROR with the whole error", logs)
	}
}

// A client that went away is logged once, by the request log line, as 499;
// the adapter adds nothing.
func TestErrors_DisconnectIsNotLogged(t *testing.T) {
	t.Parallel()

	e := newEnv()
	e.repo.Block()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	e.do(ctx, http.MethodGet, "/api/v1/subscriptions/"+uuid.Must(uuid.NewV7()).String(), "")
	if logs := e.logs.all(); len(logs) != 0 {
		t.Errorf("logs = %v, want none from the adapter", logs)
	}
}

// wantValidationErrors checks a 400 with exactly the given field errors.
func wantValidationErrors(t *testing.T, w *httptest.ResponseRecorder, want []problem.FieldError) {
	t.Helper()
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body %s", w.Code, w.Body)
	}
	p := problemOf(t, w)
	if p.Detail != "request validation failed" || !slices.Equal(p.Errors, want) {
		t.Errorf("problem = %q %v, want request validation failed %v", p.Detail, p.Errors, want)
	}
}

func errs(field, message string) []problem.FieldError {
	return []problem.FieldError{{Field: field, Message: message}}
}

// errInvalid is the domain's "invalid data" category.
var errInvalid = domain.ErrInvalid

func listAll() domain.ListQuery { return domain.ListQuery{Limit: 100} }
