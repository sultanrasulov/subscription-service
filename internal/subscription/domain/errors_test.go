package domain_test

import (
	"errors"
	"testing"

	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
)

// validationErrors is every specific validation error. The HTTP adapter maps
// them by category and by identity, so each one must wrap ErrInvalid and be
// distinct from all the others.
var validationErrors = map[string]error{
	"ErrInvalidMonth":                 domain.ErrInvalidMonth,
	"ErrYearOutOfRange":               domain.ErrYearOutOfRange,
	"ErrPriceOutOfRange":              domain.ErrPriceOutOfRange,
	"ErrNilUserID":                    domain.ErrNilUserID,
	"ErrEmptyServiceName":             domain.ErrEmptyServiceName,
	"ErrServiceNameInvalidCharacters": domain.ErrServiceNameInvalidCharacters,
	"ErrServiceNameTooLong":           domain.ErrServiceNameTooLong,
	"ErrEndBeforeStart":               domain.ErrEndBeforeStart,
	"ErrInvertedCalculationPeriod":    domain.ErrInvertedCalculationPeriod,
	"ErrNilSubscriptionID":            domain.ErrNilSubscriptionID,
}

func TestValidationErrors_AreInvalid(t *testing.T) {
	t.Parallel()

	for name, err := range validationErrors {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if !errors.Is(err, domain.ErrInvalid) {
				t.Errorf("errors.Is(%v, ErrInvalid) = false, want true", err)
			}
			if errors.Is(err, domain.ErrNotFound) {
				t.Errorf("errors.Is(%v, ErrNotFound) = true, want false", err)
			}
		})
	}
}

func TestValidationErrors_AreDistinct(t *testing.T) {
	t.Parallel()

	for name, err := range validationErrors {
		for other, otherErr := range validationErrors {
			if name != other && errors.Is(err, otherErr) {
				t.Errorf("errors.Is(%s, %s) = true, want false", name, other)
			}
		}
	}
}

// A merged category would turn a missing subscription into a 400.
func TestErrorCategories_AreDistinct(t *testing.T) {
	t.Parallel()

	if errors.Is(domain.ErrNotFound, domain.ErrInvalid) {
		t.Error("errors.Is(ErrNotFound, ErrInvalid) = true, want false")
	}
	if errors.Is(domain.ErrInvalid, domain.ErrNotFound) {
		t.Error("errors.Is(ErrInvalid, ErrNotFound) = true, want false")
	}
}
