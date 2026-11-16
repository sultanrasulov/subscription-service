package jsonbody_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/sultanrasulov/subscription-service/internal/platform/jsonbody"
)

const limit = 1 << 20

func read(t *testing.T, contentType, body string, maxBytes int64) (jsonbody.Object, error) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	return jsonbody.Read(httptest.NewRecorder(), r, maxBytes)
}

// wantError checks that err is a *jsonbody.Error with the given status and
// detail.
func wantError(t *testing.T, err error, status int, detail string) {
	t.Helper()
	var e *jsonbody.Error
	if !errors.As(err, &e) {
		t.Fatalf("Read() error = %v, want a *jsonbody.Error", err)
	}
	if e.Status != status || e.Detail != detail {
		t.Errorf("Read() error = %d %q, want %d %q", e.Status, e.Detail, status, detail)
	}
	if e.Err == nil && err.Error() != detail {
		t.Errorf("Read() error text = %q, want the detail %q", err.Error(), detail)
	}
}

func TestRead_ReadsAnObject(t *testing.T) {
	t.Parallel()

	obj, err := read(t, "application/json", `{"service_name": "Yandex Plus", "price": 400, "end_date": null, "nested": {"a": [1]}}`, limit)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	want := map[string]string{
		"service_name": `"Yandex Plus"`,
		"price":        `400`,
		"end_date":     `null`,
		"nested":       `{"a": [1]}`,
	}
	if len(obj.Fields) != len(want) {
		t.Errorf("Read() fields = %v, want %v", obj.Fields, want)
	}
	for name, raw := range want {
		if got := string(obj.Fields[name]); got != raw {
			t.Errorf("field %s = %s, want %s", name, got, raw)
		}
	}
	if len(obj.Duplicates) != 0 {
		t.Errorf("Read() duplicates = %v, want none", obj.Duplicates)
	}
}

func TestRead_ReadsAnEmptyObject(t *testing.T) {
	t.Parallel()

	obj, err := read(t, "application/json", " {} \n", limit)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if obj.Fields == nil || len(obj.Fields) != 0 {
		t.Errorf("Read() fields = %#v, want an empty map", obj.Fields)
	}
}

// RFC 8259 leaves a duplicated key undefined, and parsers that keep different
// copies can be played against each other, so every duplicate is reported. A
// key is compared after its escapes are decoded: "price" is "price".
func TestRead_ReportsDuplicates(t *testing.T) {
	t.Parallel()

	obj, err := read(t, "application/json", `{"price": 1, "b": 0, "a": 1, "price": 2, "a": 3, "a": 4, "pri\u0063e": 5}`, limit)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if want := []string{"a", "price"}; !slices.Equal(obj.Duplicates, want) {
		t.Errorf("Read() duplicates = %v, want %v, each once and sorted", obj.Duplicates, want)
	}
	if got := string(obj.Fields["price"]); got != "1" {
		t.Errorf("field price = %s, want the first value, 1", got)
	}
	if got := string(obj.Fields["a"]); got != "1" {
		t.Errorf("field a = %s, want the first value, 1", got)
	}
}

func TestRead_ContentType(t *testing.T) {
	t.Parallel()

	for _, contentType := range []string{
		"application/json",
		"application/json; charset=utf-8",
		"Application/JSON",
	} {
		t.Run("accepts "+contentType, func(t *testing.T) {
			t.Parallel()

			if _, err := read(t, contentType, `{}`, limit); err != nil {
				t.Errorf("Read() error = %v", err)
			}
		})
	}
	for _, contentType := range []string{
		"",
		"text/plain",
		"application/x-www-form-urlencoded",
		"application/json-patch+json",
		"application/json; charset",
	} {
		t.Run("rejects "+contentType, func(t *testing.T) {
			t.Parallel()

			_, err := read(t, contentType, `{}`, limit)
			wantError(t, err, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		})
	}
}

func TestRead_RejectsAnythingButOneObject(t *testing.T) {
	t.Parallel()

	const (
		notObject = "request body must be a JSON object"
		malformed = "request body is not valid JSON"
		trailing  = "request body has data after the JSON object"
	)
	for _, tc := range []struct {
		name, body, detail string
	}{
		{"empty", "", notObject},
		{"whitespace only", " \n\t ", notObject},
		{"array", `[{"price": 1}]`, notObject},
		{"string", `"price"`, notObject},
		{"number", `400`, notObject},
		{"null", `null`, notObject},
		// A body that ends inside the object, after a value, a key or a colon.
		{"ends after a value", `{"price": 1`, malformed},
		{"ends after a key", `{"price"`, malformed},
		{"ends after a colon", `{"price":`, malformed},
		{"ends after the brace", `{`, malformed},
		{"missing colon", `{"price" 1}`, malformed},
		{"unquoted key", `{price: 1}`, malformed},
		{"trailing comma", `{"price": 1,}`, malformed},
		{"bad value", `{"price": 01}`, malformed},
		{"second object", `{} {}`, trailing},
		{"trailing garbage", `{}x`, trailing},
		{"trailing bracket", `{}]`, trailing},
		{"unterminated value after the object", `{} "abc`, trailing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := read(t, "application/json", tc.body, limit)
			wantError(t, err, http.StatusBadRequest, tc.detail)
		})
	}
}

func TestRead_Size(t *testing.T) {
	t.Parallel()

	// An object of exactly n bytes: {"a":"…"}.
	object := func(n int) string {
		return `{"a":"` + strings.Repeat("x", n-len(`{"a":""}`)) + `"}`
	}
	for _, tc := range []struct {
		name string
		body string
		ok   bool
	}{
		{"exactly the limit", object(limit), true},
		{"one byte over", object(limit + 1), false},
		// The object fits; the whitespace after it does not.
		{"whitespace past the limit", object(limit-10) + strings.Repeat(" ", 20), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := read(t, "application/json", tc.body, limit)
			if tc.ok {
				if err != nil {
					t.Errorf("Read() error = %v", err)
				}
				return
			}
			wantError(t, err, http.StatusRequestEntityTooLarge, "request body too large")
		})
	}
}

// A body that cannot be read, such as a broken chunked encoding or a client
// too slow for the read timeout, is still the client's: the server only reads
// what the client sends. It is a 400 that keeps its cause for the log.
func TestRead_ReportsAFailedRead(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]io.Reader{
		"at the start":      failingBody{},
		"inside the object": io.MultiReader(strings.NewReader(`{"price": `), failingBody{}),
		"after the object":  io.MultiReader(strings.NewReader(`{"price": 1} `), failingBody{}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := httptest.NewRequest(http.MethodPost, "/", body)
			r.Header.Set("Content-Type", "application/json")
			_, err := jsonbody.Read(httptest.NewRecorder(), r, limit)
			wantError(t, err, http.StatusBadRequest, "request body could not be read")
			if !errors.Is(err, errBrokenBody) {
				t.Errorf("Read() error = %v, want one that wraps %v", err, errBrokenBody)
			}
			// The cause goes to the log with the detail.
			if want := "request body could not be read: connection reset"; err.Error() != want {
				t.Errorf("Read() error text = %q, want %q", err.Error(), want)
			}
		})
	}
}

var errBrokenBody = errors.New("connection reset")

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, errBrokenBody }

// The raw values are passed on as they came, so the caller decides their
// types; they must still be valid JSON.
func TestRead_FieldsAreValidJSON(t *testing.T) {
	t.Parallel()

	obj, err := read(t, "application/json", `{"a": 400.0, "b": "x\"y", "c": true}`, limit)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	for name, raw := range obj.Fields {
		if !json.Valid(raw) {
			t.Errorf("field %s = %s, not valid JSON", name, raw)
		}
	}
	if got := string(obj.Fields["a"]); got != "400.0" {
		t.Errorf("field a = %s, want 400.0 as sent", got)
	}
}
