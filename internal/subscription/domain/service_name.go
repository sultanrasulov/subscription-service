package domain

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxServiceNameLength is the longest service name, in characters (Unicode
// code points, not bytes).
const MaxServiceNameLength = 100

// ServiceName is the name of a subscribed service, such as "Yandex Plus".
//
// It is normalized: no leading or trailing whitespace, and single spaces
// between words. Case is preserved; names are matched case-insensitively
// where they are stored. The zero value is not a valid name; IsZero reports
// it.
type ServiceName struct {
	name string
}

// NewServiceName normalizes raw and returns it as a service name. Every
// Unicode whitespace run, including tabs, newlines and no-break spaces,
// becomes a single space, and the ends are trimmed. The result must then be:
//   - not empty, or it returns ErrEmptyServiceName;
//   - valid UTF-8 without control characters, or it returns
//     ErrServiceNameInvalidCharacters;
//   - at most MaxServiceNameLength characters, or it returns
//     ErrServiceNameTooLong.
func NewServiceName(raw string) (ServiceName, error) {
	// Normalizing first matters: tabs and newlines are control characters
	// themselves, and only here do they become spaces.
	name := strings.Join(strings.Fields(raw), " ")
	switch {
	case name == "":
		return ServiceName{}, ErrEmptyServiceName
	// A NUL character or invalid UTF-8 would fail in the database, and other
	// control characters would be stored and echoed into responses and logs.
	case !utf8.ValidString(name) || strings.ContainsFunc(name, unicode.IsControl):
		return ServiceName{}, ErrServiceNameInvalidCharacters
	case utf8.RuneCountInString(name) > MaxServiceNameLength:
		return ServiceName{}, ErrServiceNameTooLong
	}
	return ServiceName{name: name}, nil
}

// String returns the normalized name.
func (n ServiceName) String() string { return n.name }

// IsZero reports whether n is the zero value, which is not a valid name. A
// filter uses it to mean "any service".
func (n ServiceName) IsZero() bool { return n.name == "" }
