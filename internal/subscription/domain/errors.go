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
)
