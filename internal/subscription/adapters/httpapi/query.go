package httpapi

import (
	"fmt"
	"math"
	"net/url"
	"strconv"

	"github.com/sultanrasulov/subscription-service/internal/platform/problem"
	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
)

const (
	defaultLimit = 20
	maxLimit     = 100
)

// parseQuery parses a raw query string strictly. r.URL.Query() would drop a
// pair it cannot decode, such as one with an invalid escape or a ";" separator,
// and a dropped filter would show every user's subscriptions; here any such
// error fails the whole query instead.
func parseQuery(raw string) (url.Values, bool) {
	values, err := url.ParseQuery(raw)
	return values, err == nil
}

// params reads the parameters of one query string and collects their errors,
// at most one each. fieldErrors returns them in the order the endpoint
// declares, so the same mistakes give the same response whatever order the
// client sent the parameters in. Unknown parameters are ignored: proxies and
// tracking tools add them.
type params struct {
	values url.Values
	order  []string
	errors map[string]string
}

func newParams(values url.Values, order ...string) *params {
	return &params{values: values, order: order, errors: make(map[string]string)}
}

func (p *params) fail(name, message string) {
	if _, ok := p.errors[name]; !ok {
		p.errors[name] = message
	}
}

// get returns a parameter's value, or false when it is absent, which means no
// value, or when it is empty or repeated, which is an error.
func (p *params) get(name string) (string, bool) {
	values, ok := p.values[name]
	switch {
	case !ok:
		return "", false
	case len(values) > 1:
		p.fail(name, "must not be repeated")
		return "", false
	case values[0] == "":
		p.fail(name, "must not be empty")
		return "", false
	}
	return values[0], true
}

// filter reads the user_id and service_name filters. An absent one does not
// filter.
func (p *params) filter() domain.Filter {
	var f domain.Filter
	if s, ok := p.get("user_id"); ok {
		f.UserID = p.userID("user_id", s)
	}
	if s, ok := p.get("service_name"); ok {
		n, err := domain.NewServiceName(s)
		if err != nil {
			p.fail("service_name", serviceNameMessage(err))
		}
		f.ServiceName = n
	}
	return f
}

func (p *params) userID(name, s string) domain.UserID {
	id, ok := parseUUID(s)
	if !ok {
		p.fail(name, "must be a valid UUID")
		return domain.UserID{}
	}
	u, err := domain.NewUserID(id)
	if err != nil {
		p.fail(name, "must not be the nil UUID")
	}
	return u
}

// integer reads an integer parameter within [lowest, highest], or returns
// fallback when it is absent.
func (p *params) integer(name string, lowest, highest, fallback int, message string) int {
	s, ok := p.get(name)
	if !ok {
		return fallback
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < lowest || n > highest {
		p.fail(name, message)
		return fallback
	}
	return n
}

// fieldErrors returns the collected errors in the declared order.
func (p *params) fieldErrors() []problem.FieldError {
	var out []problem.FieldError
	for _, name := range p.order {
		if message, ok := p.errors[name]; ok {
			out = append(out, problem.FieldError{Field: name, Message: message})
		}
	}
	return out
}

// readList validates the parameters of a list request.
func readList(values url.Values) (domain.ListQuery, []problem.FieldError) {
	p := newParams(values, "user_id", "service_name", "limit", "offset")
	q := domain.ListQuery{
		Filter: p.filter(),
		Limit:  p.integer("limit", 1, maxLimit, defaultLimit, fmt.Sprintf("must be an integer between 1 and %d", maxLimit)),
		Offset: p.integer("offset", 0, math.MaxInt, 0, "must be a non-negative integer"),
	}
	return q, p.fieldErrors()
}
