package httpapi_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/sultanrasulov/subscription-service/internal/platform/problem"
)

const (
	otherUserID = "1b4e28ba-2fa1-41d2-883f-0016d3cca427"

	msgNotEmpty    = "must not be empty"
	msgNotRepeated = "must not be repeated"
	msgLimit       = "must be an integer between 1 and 100"
	msgOffset      = "must be a non-negative integer"
)

// listPage is a list response.
type listPage struct {
	Items  []map[string]any `json:"items"`
	Limit  int              `json:"limit"`
	Offset int              `json:"offset"`
	Total  int              `json:"total"`
}

// seed creates subscriptions through the API and returns their IDs, sorted
// as the list orders them.
func (e env) seed(t *testing.T, bodies ...map[string]string) []string {
	t.Helper()
	var ids []string
	for _, replace := range bodies {
		w := e.do(t.Context(), http.MethodPost, "/api/v1/subscriptions", createBody(replace))
		if w.Code != http.StatusCreated {
			t.Fatalf("create status = %d; body %s", w.Code, w.Body)
		}
		var created map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
			t.Fatalf("decode the created subscription: %v", err)
		}
		id, ok := created["id"].(string)
		if !ok {
			t.Fatalf("created subscription = %v, want a string id", created)
		}
		ids = append(ids, id)
	}
	// Canonical lower-case UUIDs sort as text the way their bytes do.
	slices.Sort(ids)
	return ids
}

func (e env) list(t *testing.T, query string) listPage {
	t.Helper()
	w := e.do(t.Context(), http.MethodGet, "/api/v1/subscriptions"+query, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200; body %s", query, w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), `"items":[`) {
		t.Errorf("body = %s, want items as an array", w.Body)
	}
	var page listPage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode the page: %v", err)
	}
	return page
}

func itemIDs(t *testing.T, page listPage) []string {
	t.Helper()
	ids := []string{}
	for _, item := range page.Items {
		id, ok := item["id"].(string)
		if !ok {
			t.Fatalf("item = %v, want a string id", item)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestList_FiltersAndPages(t *testing.T) {
	t.Parallel()

	// Two of validUserID's subscriptions to Yandex Plus, one of theirs to
	// Netflix, and another user's to Yandex Plus.
	e := newEnv()
	e.seed(t,
		nil,
		nil,
		map[string]string{"service_name": `"Netflix"`},
		map[string]string{"user_id": `"` + otherUserID + `"`},
	)

	for _, tc := range []struct {
		name          string
		query         string
		limit, offset int
		total, items  int
	}{
		{"defaults", "", 20, 0, 4, 4},
		{"user", "?user_id=" + otherUserID, 20, 0, 1, 1},
		{"user in upper case", "?user_id=" + strings.ToUpper(otherUserID), 20, 0, 1, 1},
		{"service in another case", "?service_name=yandex%20PLUS", 20, 0, 3, 3},
		{"service with spaces to normalize", "?service_name=%20Yandex%20%20Plus%20", 20, 0, 3, 3},
		{"user and service", "?user_id=" + validUserID + "&service_name=Netflix", 20, 0, 1, 1},
		{"a page", "?limit=2&offset=1", 2, 1, 4, 2},
		{"the largest page", "?limit=100", 100, 0, 4, 4},
		{"past the end", "?offset=10", 20, 10, 4, 0},
		{"unknown parameters ignored", "?utm_source=mail&_=1712345678", 20, 0, 4, 4},
		{"no match", "?service_name=Okko", 20, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			page := e.list(t, tc.query)
			if page.Limit != tc.limit || page.Offset != tc.offset || page.Total != tc.total || len(page.Items) != tc.items {
				t.Errorf("page = limit %d, offset %d, total %d, %d items; want %d, %d, %d, %d",
					page.Limit, page.Offset, page.Total, len(page.Items), tc.limit, tc.offset, tc.total, tc.items)
			}
		})
	}
}

func TestList_OrdersByID(t *testing.T) {
	t.Parallel()

	e := newEnv()
	want := e.seed(t, nil, nil, nil)

	page := e.list(t, "?limit=2&offset=1")
	if got := itemIDs(t, page); !slices.Equal(got, want[1:3]) {
		t.Errorf("items = %v, want %v", got, want[1:3])
	}
	if page.Items[0]["service_name"] != "Yandex Plus" || page.Items[0]["end_date"] != "12-2025" {
		t.Errorf("item = %v, want a whole subscription", page.Items[0])
	}
}

func TestList_EmptyIsAnArray(t *testing.T) {
	t.Parallel()

	page := newEnv().list(t, "")
	if page.Items == nil || len(page.Items) != 0 || page.Total != 0 {
		t.Errorf("page = %+v, want no items as an empty array, total 0", page)
	}
}

// A lenient parser drops a pair it cannot decode, so the filter would vanish
// and the list would show every user's subscriptions.
func TestList_RejectsAMalformedQuery(t *testing.T) {
	t.Parallel()

	e := newEnv()
	e.seed(t, nil)
	for _, query := range []string{
		"?user_id=%zz",
		"?user_id=" + otherUserID + ";limit=5",
		"?limit=5&user_id=%zz",
	} {
		t.Run(query, func(t *testing.T) {
			t.Parallel()

			w := e.do(t.Context(), http.MethodGet, "/api/v1/subscriptions"+query, "")
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body %s", w.Code, w.Body)
			}
			if p := problemOf(t, w); p.Detail != "query string is malformed" || len(p.Errors) != 0 {
				t.Errorf("problem = %q %v, want query string is malformed and no field errors", p.Detail, p.Errors)
			}
		})
	}
}

func TestList_RejectsInvalidParameters(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, query string
		want        []problem.FieldError
	}{
		{"empty user", "?user_id=", errs("user_id", msgNotEmpty)},
		{"repeated user", "?user_id=" + validUserID + "&user_id=" + otherUserID, errs("user_id", msgNotRepeated)},
		{"user not a UUID", "?user_id=abc", errs("user_id", msgUUID)},
		{"user in braces", "?user_id=%7B" + validUserID + "%7D", errs("user_id", msgUUID)},
		{"user without hyphens", "?user_id=" + strings.ReplaceAll(validUserID, "-", ""), errs("user_id", msgUUID)},
		{"nil user", "?user_id=00000000-0000-0000-0000-000000000000", errs("user_id", msgNilUUID)},

		{"empty service", "?service_name=", errs("service_name", msgNotEmpty)},
		{"blank service", "?service_name=%20%20", errs("service_name", msgEmpty)},
		{"service with an escape", "?service_name=Yandex%1BPlus", errs("service_name", msgCharacters)},
		{"service with invalid UTF-8", "?service_name=Yandex%FFPlus", errs("service_name", msgCharacters)},
		{"service of 101 characters", "?service_name=" + strings.Repeat("a", 101), errs("service_name", msgTooLong)},
		{"repeated service", "?service_name=Okko&service_name=Ivi", errs("service_name", msgNotRepeated)},

		{"limit 0", "?limit=0", errs("limit", msgLimit)},
		{"limit 101", "?limit=101", errs("limit", msgLimit)},
		{"negative limit", "?limit=-1", errs("limit", msgLimit)},
		{"limit not a number", "?limit=abc", errs("limit", msgLimit)},
		{"fractional limit", "?limit=1.5", errs("limit", msgLimit)},
		{"limit beyond 64 bits", "?limit=99999999999999999999", errs("limit", msgLimit)},
		{"empty limit", "?limit=", errs("limit", msgNotEmpty)},
		{"repeated limit", "?limit=1&limit=2", errs("limit", msgNotRepeated)},

		{"negative offset", "?offset=-1", errs("offset", msgOffset)},
		{"offset not a number", "?offset=abc", errs("offset", msgOffset)},
		{"offset beyond 64 bits", "?offset=99999999999999999999", errs("offset", msgOffset)},
		{"empty offset", "?offset=", errs("offset", msgNotEmpty)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := newEnv().do(t.Context(), http.MethodGet, "/api/v1/subscriptions"+tc.query, "")
			wantValidationErrors(t, w, tc.want)
		})
	}
}

// Every parameter error comes in one response, in a fixed order, whatever
// order the client sent the parameters in. Each query is sent several times:
// Go randomizes the order of a map, so an implementation that leaked it would
// pass a single try by chance.
func TestList_ReportsEveryErrorInAFixedOrder(t *testing.T) {
	t.Parallel()

	want := []problem.FieldError{
		{Field: "user_id", Message: msgUUID},
		{Field: "service_name", Message: msgNotEmpty},
		{Field: "limit", Message: msgLimit},
		{Field: "offset", Message: msgOffset},
	}
	for _, query := range []string{
		"?offset=-1&limit=0&service_name=&user_id=abc",
		"?user_id=abc&service_name=&limit=0&offset=-1",
		"?limit=0&user_id=abc&offset=-1&service_name=",
	} {
		t.Run(query, func(t *testing.T) {
			t.Parallel()

			e := newEnv()
			for range 10 {
				w := e.do(t.Context(), http.MethodGet, "/api/v1/subscriptions"+query, "")
				wantValidationErrors(t, w, want)
			}
		})
	}
}
