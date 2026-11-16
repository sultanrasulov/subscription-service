package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sultanrasulov/subscription-service/internal/platform/jsonbody"
	"github.com/sultanrasulov/subscription-service/internal/platform/problem"
	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
)

// createRequest is the body of a create request. It documents the request in
// Swagger, and its tags define the body's fields: their order and which are
// required. The body itself is read field by field, so that every mistake can
// be reported, but from this one definition, so the documentation and the
// validation cannot drift apart.
type createRequest struct {
	ServiceName string `json:"service_name" validate:"required" maxLength:"100" example:"Yandex Plus"`
	// The monthly price in whole rubles.
	Price  int64  `json:"price" validate:"required" minimum:"0" maximum:"2147483647" example:"400"`
	UserID string `json:"user_id" validate:"required" format:"uuid" example:"60601fee-2bf1-4721-ae6f-7636e79a0cba"`
	// The first month, MM-YYYY.
	StartDate string `json:"start_date" validate:"required" example:"07-2025"`
	// The last month, MM-YYYY, inclusive; omitted for an open-ended subscription.
	EndDate string `json:"end_date,omitempty" example:"12-2025"`
}

// field is one field of a request body.
type field struct {
	name     string
	required bool
}

// schemaOf returns the fields of the request type T in declaration order,
// from their json and validate tags.
func schemaOf[T any]() []field {
	t := reflect.TypeFor[T]()
	fields := make([]field, 0, t.NumField())
	for i := range t.NumField() {
		tag := t.Field(i).Tag
		name, _, _ := strings.Cut(tag.Get("json"), ",")
		fields = append(fields, field{name: name, required: tag.Get("validate") == "required"})
	}
	return fields
}

var createSchema = schemaOf[createRequest]()

// updateRequest is the body of an update request, a full replacement. It has
// no user_id: the user of a subscription never changes, so one sent anyway is
// an unknown field.
type updateRequest struct {
	ServiceName string `json:"service_name" validate:"required" maxLength:"100" example:"Kinopoisk"`
	// The monthly price in whole rubles.
	Price int64 `json:"price" validate:"required" minimum:"0" maximum:"2147483647" example:"299"`
	// The first month, MM-YYYY.
	StartDate string `json:"start_date" validate:"required" example:"01-2026"`
	// The last month, MM-YYYY, inclusive; omitted for an open-ended subscription.
	EndDate string `json:"end_date,omitempty" example:"03-2026"`
}

var updateSchema = schemaOf[updateRequest]()

// createInput is a validated create request.
type createInput struct {
	userID domain.UserID
	name   domain.ServiceName
	price  domain.Price
	period domain.SubscriptionPeriod
}

// readCreate validates a create body. The input is usable only when there are
// no field errors.
func readCreate(obj jsonbody.Object) (createInput, []problem.FieldError) {
	b := newBody(obj, createSchema)
	in := createInput{
		name:   b.serviceName("service_name"),
		price:  b.price("price"),
		userID: b.userID("user_id"),
		period: b.period("start_date", "end_date"),
	}
	return in, b.fieldErrors()
}

// updateInput is a validated update request.
type updateInput struct {
	name   domain.ServiceName
	price  domain.Price
	period domain.SubscriptionPeriod
}

// readUpdate validates an update body. The input is usable only when there
// are no field errors.
func readUpdate(obj jsonbody.Object) (updateInput, []problem.FieldError) {
	b := newBody(obj, updateSchema)
	in := updateInput{
		name:   b.serviceName("service_name"),
		price:  b.price("price"),
		period: b.period("start_date", "end_date"),
	}
	return in, b.fieldErrors()
}

// body reads the fields of one request body and collects their errors. Each
// field gets at most one error, and fieldErrors returns them in a fixed order:
// the schema's fields in its order, then unknown fields, then duplicated ones,
// each alphabetically. The same mistakes then give the same response, whatever
// order the client sent the keys in.
type body struct {
	obj    jsonbody.Object
	schema []field
	errors map[string]string
}

func newBody(obj jsonbody.Object, schema []field) *body {
	return &body{obj: obj, schema: schema, errors: make(map[string]string)}
}

func (b *body) fail(name, message string) {
	if _, ok := b.errors[name]; !ok {
		b.errors[name] = message
	}
}

// value returns a field's raw value, or false when there is none to read: the
// field is missing or null, which reports a required field, or it is
// duplicated, which fieldErrors reports and whose value is not read, because
// it is unclear which copy counts.
func (b *body) value(name string) (json.RawMessage, bool) {
	if slices.Contains(b.obj.Duplicates, name) {
		return nil, false
	}
	raw, ok := b.obj.Fields[name]
	if !ok || string(raw) == "null" {
		if b.required(name) {
			b.fail(name, "is required")
		}
		return nil, false
	}
	return raw, true
}

func (b *body) required(name string) bool {
	for _, f := range b.schema {
		if f.name == name {
			return f.required
		}
	}
	return false
}

// str returns a field's value as a string, or reports typeMessage when it is
// some other JSON type.
func (b *body) str(name, typeMessage string) (string, bool) {
	raw, ok := b.value(name)
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		b.fail(name, typeMessage)
		return "", false
	}
	return s, true
}

func (b *body) serviceName(name string) domain.ServiceName {
	s, ok := b.str(name, "must be a string")
	if !ok {
		return domain.ServiceName{}
	}
	n, err := domain.NewServiceName(s)
	if err != nil {
		b.fail(name, serviceNameMessage(err))
	}
	return n
}

func serviceNameMessage(err error) string {
	switch {
	case errors.Is(err, domain.ErrEmptyServiceName):
		return "must not be empty"
	case errors.Is(err, domain.ErrServiceNameTooLong):
		return fmt.Sprintf("must be at most %d characters", domain.MaxServiceNameLength)
	default:
		return "must not contain control characters or invalid UTF-8"
	}
}

func (b *body) price(name string) domain.Price {
	raw, ok := b.value(name)
	if !ok {
		return domain.Price{}
	}
	// One message for the wrong type and for the range alike: either way the
	// client learns what a price is.
	message := fmt.Sprintf("must be an integer between 0 and %d", domain.MaxPrice)
	// ParseInt takes only base-10 digits, so a fraction (400.0), an exponent
	// (4e2), a string or a boolean is not a price, and neither is a number
	// beyond 64 bits.
	rubles, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil {
		b.fail(name, message)
		return domain.Price{}
	}
	p, err := domain.NewPrice(rubles)
	if err != nil {
		b.fail(name, message)
	}
	return p
}

func (b *body) userID(name string) domain.UserID {
	s, ok := b.str(name, "must be a valid UUID")
	if !ok {
		return domain.UserID{}
	}
	id, ok := parseUUID(s)
	if !ok {
		b.fail(name, "must be a valid UUID")
		return domain.UserID{}
	}
	u, err := domain.NewUserID(id)
	if err != nil {
		b.fail(name, "must not be the nil UUID")
	}
	return u
}

// period reads a subscription period from its start and its optional end. A
// missing or null end makes it open-ended. The end is compared with the start
// only when both are valid months.
func (b *body) period(startName, endName string) domain.SubscriptionPeriod {
	start, startOK := b.month(startName)
	if _, present := b.obj.Fields[endName]; !present || string(b.obj.Fields[endName]) == "null" {
		if !startOK {
			return domain.SubscriptionPeriod{}
		}
		p, err := domain.NewOpenEndedSubscriptionPeriod(start)
		if err != nil {
			b.fail(startName, monthMessage(err))
		}
		return p
	}
	end, endOK := b.month(endName)
	if !startOK || !endOK {
		return domain.SubscriptionPeriod{}
	}
	p, err := domain.NewSubscriptionPeriod(start, end)
	if errors.Is(err, domain.ErrEndBeforeStart) {
		b.fail(endName, "must not be before "+startName)
	} else if err != nil {
		b.fail(endName, monthMessage(err))
	}
	return p
}

// monthFormat matches MM-YYYY: two digits, a hyphen, four digits.
var monthFormat = regexp.MustCompile(`^[0-9]{2}-[0-9]{4}$`)

// monthFormatMessage reports a month that is not written as MM-YYYY.
const monthFormatMessage = "must be in MM-YYYY format"

func (b *body) month(name string) (domain.MonthYear, bool) {
	s, ok := b.str(name, monthFormatMessage)
	if !ok {
		return domain.MonthYear{}, false
	}
	m, message, ok := parseMonth(s)
	if !ok {
		b.fail(name, message)
	}
	return m, ok
}

// parseMonth reads a month written as MM-YYYY, for the body and the query
// alike, so both report the same mistakes the same way. It returns the month,
// or false and the message that says what is wrong.
func parseMonth(s string) (domain.MonthYear, string, bool) {
	if !monthFormat.MatchString(s) {
		return domain.MonthYear{}, monthFormatMessage, false
	}
	// The format guarantees both are digits.
	month, _ := strconv.Atoi(s[:2])
	year, _ := strconv.Atoi(s[3:])
	m, err := domain.NewMonthYear(year, time.Month(month))
	if err != nil {
		return domain.MonthYear{}, monthMessage(err), false
	}
	return m, "", true
}

func monthMessage(err error) string {
	if errors.Is(err, domain.ErrYearOutOfRange) {
		return fmt.Sprintf("year must be between %d and %d", domain.MinYear, domain.MaxYear)
	}
	return "month must be between 01 and 12"
}

// fieldErrors returns the collected errors in their fixed order.
func (b *body) fieldErrors() []problem.FieldError {
	var out []problem.FieldError
	known := make(map[string]bool, len(b.schema))
	for _, f := range b.schema {
		known[f.name] = true
		if message, ok := b.errors[f.name]; ok {
			out = append(out, problem.FieldError{Field: f.name, Message: message})
		}
	}
	// A duplicated key gets only its duplicate error, known or not.
	var unknown []string
	for name := range b.obj.Fields {
		if !known[name] && !slices.Contains(b.obj.Duplicates, name) {
			unknown = append(unknown, name)
		}
	}
	slices.Sort(unknown)
	for _, name := range unknown {
		out = append(out, problem.FieldError{Field: name, Message: "unknown field"})
	}
	for _, name := range b.obj.Duplicates {
		out = append(out, problem.FieldError{Field: name, Message: "duplicate field"})
	}
	return out
}

// parseUUID accepts only the canonical form of RFC 9562: 36 characters,
// 8-4-4-4-12, hex digits in either case. uuid.Parse also takes the braced,
// urn:uuid: and 32-digit forms; one resource then has one URL.
func parseUUID(s string) (uuid.UUID, bool) {
	if len(s) != 36 {
		return uuid.UUID{}, false
	}
	id, err := uuid.Parse(s)
	return id, err == nil
}
