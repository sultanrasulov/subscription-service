package domain

import (
	"errors"
	"fmt"
)

// The two error categories. Every error the domain returns wraps exactly one
// of them, so a caller can handle a whole category with errors.Is without
// knowing each specific error.
var (
	// ErrInvalid is the category of every validation failure.
	ErrInvalid = errors.New("invalid data")
	// ErrNotFound is the category of a subscription that does not exist.
	ErrNotFound = errors.New("not found")
)

// The specific validation errors. Each one wraps ErrInvalid and is a distinct
// value, so a caller can tell them apart with errors.Is, for example to report
// each on its own field.
var (
	// ErrInvalidMonth reports a month outside January to December.
	ErrInvalidMonth = fmt.Errorf("%w: month must be between 1 and 12", ErrInvalid)
	// ErrYearOutOfRange reports a year outside MinYear to MaxYear.
	ErrYearOutOfRange = fmt.Errorf("%w: year must be between %d and %d", ErrInvalid, MinYear, MaxYear)
	// ErrPriceOutOfRange reports a price below 0 or above MaxPrice.
	ErrPriceOutOfRange = fmt.Errorf("%w: price must be between 0 and %d", ErrInvalid, MaxPrice)
	// ErrNilUserID reports the nil UUID as a user ID.
	ErrNilUserID = fmt.Errorf("%w: user ID must not be the nil UUID", ErrInvalid)
	// ErrEmptyServiceName reports a service name that is empty or only
	// whitespace.
	ErrEmptyServiceName = fmt.Errorf("%w: service name must not be empty", ErrInvalid)
	// ErrServiceNameInvalidCharacters reports a service name with a control
	// character or with bytes that are not valid UTF-8.
	ErrServiceNameInvalidCharacters = fmt.Errorf("%w: service name must not contain control characters or invalid UTF-8", ErrInvalid)
	// ErrServiceNameTooLong reports a service name longer than
	// MaxServiceNameLength characters.
	ErrServiceNameTooLong = fmt.Errorf("%w: service name must be at most %d characters", ErrInvalid, MaxServiceNameLength)
	// ErrEndBeforeStart reports a subscription period that ends before it
	// starts.
	ErrEndBeforeStart = fmt.Errorf("%w: end month must not be before the start month", ErrInvalid)
	// ErrInvertedCalculationPeriod reports a calculation period whose last
	// month is before its first.
	ErrInvertedCalculationPeriod = fmt.Errorf("%w: calculation period must not end before it starts", ErrInvalid)
)
