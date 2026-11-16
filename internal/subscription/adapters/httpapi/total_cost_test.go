package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sultanrasulov/subscription-service/internal/platform/problem"
)

const (
	totalCostPath = "/api/v1/subscriptions/total-cost"

	msgFromBeforeTo = "must not be before from"
)

func TestTotalCost_SumsTheMatchingSubscriptions(t *testing.T) {
	t.Parallel()

	// validUserID's Yandex Plus, 07-2025…12-2025 at 400, and Netflix, open-ended
	// from 11-2025 at 599; another user's Yandex Plus as validUserID's.
	e := newEnv()
	e.seed(t,
		nil,
		map[string]string{"service_name": `"Netflix"`, "price": `599`, "start_date": `"11-2025"`, "end_date": ""},
		map[string]string{"user_id": `"` + otherUserID + `"`},
	)

	for _, tc := range []struct {
		name, query string
		want        int64
	}{
		{"a user over a year", "?from=01-2025&to=12-2025&user_id=" + validUserID, 6*400 + 2*599},
		{"every user", "?from=01-2025&to=12-2025", 2*6*400 + 2*599},
		{"the first month, inclusive", "?from=07-2025&to=07-2025&user_id=" + validUserID, 400},
		{"the last month, inclusive", "?from=12-2025&to=12-2025&user_id=" + validUserID, 400 + 599},
		{"from and to in their places", "?from=01-2025&to=08-2025&user_id=" + validUserID, 2 * 400},
		{"an open-ended subscription until to", "?from=01-2026&to=03-2026&user_id=" + validUserID, 3 * 599},
		{"a service in another case", "?from=01-2025&to=12-2025&service_name=yandex%20PLUS", 2 * 6 * 400},
		{"a user and a service", "?from=01-2025&to=12-2025&user_id=" + validUserID + "&service_name=Netflix", 2 * 599},
		{"no match", "?from=01-2024&to=12-2024", 0},
		{"unknown parameters ignored", "?from=01-2025&to=12-2025&user_id=" + validUserID + "&utm_source=mail&_=1712345678", 6*400 + 2*599},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := e.totalCost(t, tc.query); got != tc.want {
				t.Errorf("total_cost = %d, want %d", got, tc.want)
			}
		})
	}
}

// The response carries the total alone: no currency, no echo of the query.
func TestTotalCost_RespondsWithTheTotalAlone(t *testing.T) {
	t.Parallel()

	e := newEnv()
	e.seed(t, nil)
	w := e.do(t.Context(), http.MethodGet, totalCostPath+"?from=01-2025&to=12-2025", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", w.Code, w.Body)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if got := strings.TrimSpace(w.Body.String()); got != `{"total_cost":2400}` {
		t.Errorf("body = %s, want {\"total_cost\":2400}", got)
	}
}

// The literal total-cost segment wins over {id} for GET. The other methods
// reach the {id} routes, where total-cost is not a UUID.
func TestTotalCost_Routing(t *testing.T) {
	t.Parallel()

	e := newEnv()
	w := e.do(t.Context(), http.MethodGet, totalCostPath, "")
	wantValidationErrors(t, w, []problem.FieldError{
		{Field: "from", Message: msgRequired},
		{Field: "to", Message: msgRequired},
	})

	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			w := e.do(t.Context(), method, totalCostPath, updateBody(nil))
			wantValidationErrors(t, w, errs("id", msgUUID))
		})
	}
}

func TestTotalCost_RejectsInvalidParameters(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, query string
		want        []problem.FieldError
	}{
		{"absent from", "?to=12-2025", errs("from", msgRequired)},
		{"absent to", "?from=01-2025", errs("to", msgRequired)},
		{"empty from", "?from=&to=12-2025", errs("from", msgNotEmpty)},
		{"empty to", "?from=01-2025&to=", errs("to", msgNotEmpty)},
		{"repeated from", "?from=01-2025&from=02-2025&to=12-2025", errs("from", msgNotRepeated)},
		{"repeated to", "?from=01-2025&to=11-2025&to=12-2025", errs("to", msgNotRepeated)},

		{"year first", "?from=2025-01&to=12-2025", errs("from", msgMonthFormat)},
		{"one-digit month", "?from=1-2025&to=12-2025", errs("from", msgMonthFormat)},
		{"two-digit year", "?from=01-2025&to=12-25", errs("to", msgMonthFormat)},
		{"text around the month", "?from=x01-2025&to=12-2025x", []problem.FieldError{
			{Field: "from", Message: msgMonthFormat},
			{Field: "to", Message: msgMonthFormat},
		}},
		{"month 00", "?from=00-2025&to=12-2025", errs("from", msgMonth)},
		{"month 13", "?from=01-2025&to=13-2025", errs("to", msgMonth)},
		{"year 1999", "?from=01-1999&to=12-2025", errs("from", msgYear)},
		{"year 2101", "?from=01-2025&to=01-2101", errs("to", msgYear)},

		{"inverted period", "?from=12-2025&to=01-2025", errs("to", msgFromBeforeTo)},
		{"inverted, from invalid", "?from=13-2025&to=01-2025", errs("from", msgMonth)},
		{"inverted, to invalid", "?from=12-2025&to=01-1999", errs("to", msgYear)},

		{"invalid user", "?from=01-2025&to=12-2025&user_id=abc", errs("user_id", msgUUID)},
		{"blank service", "?from=01-2025&to=12-2025&service_name=%20", errs("service_name", msgEmpty)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := newEnv().do(t.Context(), http.MethodGet, totalCostPath+tc.query, "")
			wantValidationErrors(t, w, tc.want)
		})
	}
}

// Every parameter error comes in one response, in a fixed order, whatever
// order the client sent the parameters in. Each query is sent several times,
// so an order leaked from a map cannot pass by chance.
func TestTotalCost_ReportsEveryErrorInAFixedOrder(t *testing.T) {
	t.Parallel()

	want := []problem.FieldError{
		{Field: "user_id", Message: msgUUID},
		{Field: "service_name", Message: msgNotEmpty},
		{Field: "from", Message: msgMonthFormat},
		{Field: "to", Message: msgNotEmpty},
	}
	for _, query := range []string{
		"?to=&from=2025&service_name=&user_id=abc",
		"?user_id=abc&service_name=&from=2025&to=",
		"?from=2025&user_id=abc&to=&service_name=",
	} {
		t.Run(query, func(t *testing.T) {
			t.Parallel()

			e := newEnv()
			for range 10 {
				w := e.do(t.Context(), http.MethodGet, totalCostPath+query, "")
				wantValidationErrors(t, w, want)
			}
		})
	}
}

// A lenient parser would drop the broken pair, and with it the user filter, so
// the total would cover every user.
func TestTotalCost_RejectsAMalformedQuery(t *testing.T) {
	t.Parallel()

	e := newEnv()
	e.seed(t, nil)
	for _, query := range []string{
		"?from=01-2025&to=12-2025&user_id=%zz",
		"?from=01-2025&to=12-2025;user_id=" + otherUserID,
	} {
		t.Run(query, func(t *testing.T) {
			t.Parallel()

			w := e.do(t.Context(), http.MethodGet, totalCostPath+query, "")
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body %s", w.Code, w.Body)
			}
			if p := problemOf(t, w); p.Detail != "query string is malformed" || len(p.Errors) != 0 {
				t.Errorf("problem = %q %v, want query string is malformed and no field errors", p.Detail, p.Errors)
			}
		})
	}
}

func TestTotalCost_DeadlineIsA503(t *testing.T) {
	t.Parallel()

	e := newEnv()
	e.repo.Block()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	w := e.do(ctx, http.MethodGet, totalCostPath+"?from=01-2025&to=12-2025", "")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body %s", w.Code, w.Body)
	}
	logs := e.logs.all()
	if len(logs) != 1 || logs[0]["level"] != "WARN" || logs[0]["operation"] != "calculate total cost" {
		t.Errorf("logs = %v, want one WARN naming calculate total cost", logs)
	}
}

func TestTotalCost_StoreFailureIsA500(t *testing.T) {
	t.Parallel()

	e := newEnv()
	e.repo.Fail(errors.New("connection refused"))

	w := e.do(t.Context(), http.MethodGet, totalCostPath+"?from=01-2025&to=12-2025", "")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body %s", w.Code, w.Body)
	}
	logs := e.logs.all()
	if len(logs) != 1 || logs[0]["level"] != "ERROR" || logs[0]["operation"] != "calculate total cost" ||
		!strings.Contains(logs[0]["error"], "connection refused") {
		t.Errorf("logs = %v, want one ERROR naming calculate total cost, with the whole error", logs)
	}
}

func TestTotalCost_DisconnectIsNotLogged(t *testing.T) {
	t.Parallel()

	e := newEnv()
	e.repo.Block()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	e.do(ctx, http.MethodGet, totalCostPath+"?from=01-2025&to=12-2025", "")
	if logs := e.logs.all(); len(logs) != 0 {
		t.Errorf("logs = %v, want none from the adapter", logs)
	}
}

// totalCost asks for a total cost and returns it.
func (e env) totalCost(t *testing.T, query string) int64 {
	t.Helper()
	w := e.do(t.Context(), http.MethodGet, totalCostPath+query, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200; body %s", query, w.Code, w.Body)
	}
	var resp struct {
		TotalCost *int64 `json:"total_cost"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode the body: %v", err)
	}
	if resp.TotalCost == nil {
		t.Fatalf("body = %s, want a total_cost", w.Body)
	}
	return *resp.TotalCost
}
